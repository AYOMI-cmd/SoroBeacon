# Federation

Forwards alerts to **another SoroBeacon instance**. It is the only channel
that talks to SoroBeacon itself rather than to a chat service, a mailbox or
an endpoint of your own: the sending instance decides what matched, and the
receiving instance is handed the result.

## Status: it does not cross between two instances yet

Two open bugs stop an alert from arriving, and both are visible in the first
delivery attempt:

1. **The sender sends an empty `origin`**
   ([#506](https://github.com/sorotrail/SoroBeacon/issues/506)). The
   receiving instance requires an origin and answers
   `400 {"error":"origin is required"}`.
2. **The receiver stores the alert against the sender's ids**
   ([#526](https://github.com/sorotrail/SoroBeacon/issues/526)). The alert's
   `monitor_id` and `rule_id` come from the sending instance and do not exist
   in the receiver's database, so the insert fails and the receiver answers
   `404`.

So this page describes the configuration and the wire format, and says
plainly what works and what does not. It is not a description of a feature
you can rely on today.

## What works

* The channel is created and its configuration is checked: a missing `url` or
  `token` is a `400` at create time, not a failure at delivery time.
* The sender builds the right request: `POST <url>/api/v1/ingest` with
  `Authorization: Bearer <token>` and the alert wrapped in the federated
  envelope.
* The receiving instance authenticates the caller, enforces the hop limit
  and stamps the alert with its origin, and refuses a payload that is not a
  federated alert.
* The failure is visible: the delivery attempt is recorded, and the channel's
  health (`consecutive_failures`, `last_error`) says what the other instance
  answered.

## Setup

1. On the **receiving** instance, take a token that may write alerts. An
   instance-wide `API_TOKEN` works; a scoped token needs the `alerts:write`
   scope (see [Authorization](../reference/authorization.md)). Give the sender
   its own token, not the operator's.
2. On the **sending** instance, create the channel:

```sh
curl -s -X POST localhost:8080/api/v1/channels -d '{
  "name": "to-central",
  "type": "federation",
  "config": {"url": "http://beacon-b.internal:8080", "token": "s3cr3t-token-value"}
}'
```

3. Verify — and read the answer, because today it is the bug report:

```sh
curl -s -X POST localhost:8080/api/v1/channels/4/test
```

A working federation answers `{"status":"sent"}`. Today it answers
`502` with the receiving instance's complaint inside it:

```json
{"status":"failed","error":"federation: status 400: {\"code\":\"Bad Request\",\"error\":\"origin is required\",\"request_id\":\"…\"}"}
```

## Config

| Key | Required | Description |
| --- | --- | --- |
| `url` | yes | Base URL of the **receiving** instance, e.g. `http://beacon-b.internal:8080`. The sender appends `/api/v1/ingest`; a trailing slash is ignored. Treated as a secret (it often embeds a token in a path). |
| `token` | yes | Bearer token the sender presents to the receiving instance. Treated as a secret. |

Plain `http://` is accepted, so two instances on a private network work
without TLS. Do not send the token over plain `http://` on an untrusted
network — put the receiving instance behind TLS, or reach it over a private
link.

## The wire format

Every delivery is a `POST` to `<url>/api/v1/ingest` with the alert wrapped
in an envelope that carries the origin and the hop count:

```json
{
  "alert": {
    "id": 42,
    "monitor_id": 1,
    "monitor_name": "Token treasury",
    "rule_id": 3,
    "rule_type": "value_threshold",
    "event_id": "0000015985348674617345-0000000001",
    "contract_id": "CA7QYNF7SOWQ3GLR2BGMZEHXAVIRZA4KVWLTJJFC7MGXUA74P7UJUWDA",
    "event_name": "transfer",
    "ledger": 3721765,
    "tx_hash": "4f2a...",
    "payload": {"topics": ["transfer", "..."], "value": "..."},
    "created_at": "2026-07-21T09:41:32Z"
  },
  "origin": "http://beacon-a.internal:8080",
  "hop_count": 1
}
```

The headers are `Authorization: Bearer <token>` and `Content-Type:
application/json`.

## The receiving side

`POST /api/v1/ingest` is a normal API route: it needs a credential, and a
scoped token needs the `alerts:write` scope. It answers `201` with the
outcome, the stored alert's id and the origin it was given, or `400` when
the payload is not a federated alert it will accept:

| Condition | Answer |
| --- | --- |
| No `event_id` in the alert | `400 {"error":"event_id is required"}` |
| No `origin` | `400 {"error":"origin is required"}` |
| `hop_count` at or above the limit | `400 {"error":"hop limit reached; dropping alert to prevent loop"}` |
| The alert's monitor or rule is unknown here | `404 {"error":"not found"}` — the foreign-key problem above |

**Rules are not re-evaluated on the receiver.** The sending instance already
decided what matched; the receiver stores the result. An event that would
match one of the receiver's own rules does not fire that rule, and the
receiver's rules are not consulted at all.

**A federated alert is stored, not delivered.** The ingest endpoint writes
the alert row and stops. It does not fan the alert out to the receiving
instance's channels, so a federated alert shows up in the receiver's alert
history (`GET /api/v1/alerts`) but does not page anybody there. Delivery is
the polling path's job, and the ingest path does not go through it.

The payload of a stored federated alert carries three extra keys, so the
receiver's dashboard and API can tell where it came from:

```json
{
  "contract_id": "CA7QYNF7…",
  "_federated": true,
  "_federation_origin": "http://beacon-a.internal:8080",
  "_federation_hops": 1
}
```

## The hop limit

`MaxHopCount` is 8 (`internal/notify/federation.go`). It exists so a chain
of instances forwarding to each other terminates instead of amplifying an
alert forever: the receiver refuses any alert whose `hop_count` has reached
the limit, and `notify.ForwardFederatedAlert` refuses to send one.

The counter does not move today. The sender always sends `hop_count: 1`, and
nothing re-forwards a federated alert: `ForwardFederatedAlert` exists and is
not called by the ingest path, so a received alert is stored and stops there.
The limit is enforced on the receiving side and is ready for the forwarding
that has not been built.

## Troubleshooting

| Symptom | What it means |
| --- | --- |
| `400 {"details":[{"field":"config","reason":"federation: token is required"}]}` at create time | The channel config is incomplete. `url` and `token` are both required. |
| `502` with `federation: status 400: …"origin is required"` | The delivery reached the other instance and was refused: the sender's empty-origin bug ([#506](https://github.com/sorotrail/SoroBeacon/issues/506)). |
| `502` with `federation: status 404: …"not found"` | The other instance could not store the alert: the foreign monitor/rule ids ([#526](https://github.com/sorotrail/SoroBeacon/issues/526)). |
| `502` with a connection error | The other instance is not reachable at the `url` you configured, or it is behind TLS and the URL says `http://`. The path is `<url>/api/v1/ingest` — configure the base URL, not the full path. |
| The channel's `consecutive_failures` climbs | Every attempt failed; `last_error` carries the other instance's answer, which is the fastest way to tell the two bugs apart. |

The delivery attempt itself is on the sender:
`GET /api/v1/alerts/{id}/deliveries` lists the attempts for the alert that
triggered the delivery, and `GET /api/v1/channels/{id}` carries the channel's
health.
