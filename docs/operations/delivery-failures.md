# Diagnosing Failed Alert Deliveries

When an alert fires but never arrives at its destination, follow this triage order to identify the root cause. The system records every delivery attempt with structured data — use that instead of guessing.

## Triage Order

1. **Did the alert fire at all?**
2. **Was a delivery attempted?**
3. **Did the target accept it?**

Do not skip steps. Each step narrows the problem space and uses recorded data, not credentials.

---

## Step 1: Did the Alert Fire?

Check the **monitor's last matched time** and the **alert list**.

### Dashboard
- Open the monitor detail page → `Last matched` timestamp
- Go to **Alerts** → filter by monitor → confirm the alert exists with `Created At` near the expected time

### API
```
GET /api/v1/monitors/{id}
GET /api/v1/alerts?monitor_id={id}&limit=10
```

If no alert exists, the rule didn't match. Check:
- Rule params (event name, thresholds, etc.)
- Contract IDs on the monitor
- Poller logs for evaluation errors

---

## Step 2: Was a Delivery Attempted?

Every alert delivery creates a **delivery attempt record**. This is the single source of truth.

### Dashboard
- Open the alert detail → **Delivery attempts** section
- Shows: channel name, status, response code, timestamp, error snippet

### API
```
GET /api/v1/alerts/{id}/deliveries
```

Response includes:
```json
{
  "id": 123,
  "alert_id": 456,
  "channel_id": 789,
  "channel_name": "ops-slack",
  "status": "failed",
  "response_code": 401,
  "response_snippet": "invalid_auth",
  "created_at": "2026-09-30T12:34:56Z"
}
```

**Key fields:**
| Field | Meaning |
|-------|---------|
| `status` | `sent`, `failed`, `pending`, `suppressed` |
| `response_code` | HTTP status from target (2xx = accepted, 4xx/5xx = rejected) |
| `response_snippet` | First ~200 chars of response body (sanitized) |
| `channel_name` | Which channel was used |

If **no delivery attempt exists** for a channel:
- Channel may not be attached to the monitor
- Channel may be disabled
- Monitor may have no enabled channels
- Check `GET /api/v1/monitors/{id}` → `channel_ids`

---

## Step 3: Did the Target Accept It?

Read the `response_code` and `response_snippet` from the delivery attempt.

### Common Failure Modes by Channel Type

| Channel | Typical Failure Codes | Common Causes |
|---------|----------------------|---------------|
| **Slack** | 401, 403, 404 | Expired/invalid webhook URL, revoked app, deleted channel |
| **Discord** | 401, 403, 404 | Invalid webhook, missing permissions, deleted channel |
| **Telegram** | 401, 403, 400 | Revoked bot token, blocked bot, invalid chat_id |
| **Email (SMTP)** | 550, 554, 421 | Relay rejects sender, auth failed, greylisting |
| **Generic Webhook** | 401, 403, 404, 500 | Target service down, auth expired, payload too large |
| **PagerDuty** | 401, 403, 422 | Invalid integration key, malformed payload |
| **Matrix** | 401, 403, 404 | Expired access token, room not found |
| **Opsgenie** | 401, 403 | Invalid API key |
| **Gotify** | 401, 403 | Expired token |
| **All** | 0 (no response) | Network timeout, DNS failure, firewall |

### Interpreting Response Codes

| Code Range | Meaning | Action |
|------------|---------|--------|
| 2xx | **Success** — target accepted delivery | If still not visible, check target service (spam folder, Slack thread, etc.) |
| 401/403 | **Auth failure** — credential invalid/revoked | Rotate the webhook URL / bot token / API key in channel config |
| 404 | **Not found** — endpoint/channel deleted | Recreate the webhook/channel and update config |
| 429 | **Rate limited** — target rejecting volume | Check channel rate limit config; enable digest mode |
| 5xx | **Target error** — service unavailable | Retry later; check target status page |
| 0 / timeout | **Network failure** | Check connectivity, DNS, firewall |

---

## Per-Channel Failure Modes

### Slack
- **401/403**: Webhook URL revoked or app uninstalled → Generate new webhook in Slack app settings
- **404**: Channel deleted → Create new channel or update webhook to different channel
- **Rate limits**: Slack allows ~1 msg/sec per webhook → Use digest mode or increase `NOTIFY_RATE_LIMIT_SLACK_RPS`

### Discord
- **401/403**: Webhook deleted or missing permissions → Recreate webhook in Server Settings → Integrations
- **400**: Payload too large (>2000 chars) → Check message template length

### Telegram
- **401**: Bot token revoked → Get new token from @BotFather
- **403**: Bot blocked by user or not in group → Unblock or add bot to group
- **400**: Invalid chat_id → Verify chat_id in channel config

### Email (SMTP)
- **550/554**: Sender rejected → Check `FROM` address, SPF/DKIM, relay allowlist
- **421**: Greylisting / temporary deferral → Retry usually succeeds; check `ChannelDisableAfterFailures`
- **Auth failure**: Wrong username/password → Update SMTP credentials

### Generic Webhook
- Check target service docs for expected payload format
- Verify `Content-Type` header (configured in channel template)
- Check payload size limits

---

## Channel Health & Auto-Disable

SoroBeacon tracks **consecutive permanent failures** (401, 403, 404) per channel.

### Configuration
```
CHANNEL_DISABLE_AFTER_FAILURES=5  # default: 0 (disabled)
```

When threshold is reached:
- Channel is marked `enabled=false` automatically
- `disabled_at` timestamp recorded
- No further deliveries attempted until re-enabled

### Check Channel Health
```
GET /api/v1/channels
```
Response includes:
```json
{
  "id": 1,
  "name": "ops-slack",
  "enabled": false,
  "consecutive_failures": 7,
  "consecutive_permanent_failures": 7,
  "last_error": "status 401: Unauthorized",
  "last_error_at": "2026-09-30T12:00:00Z",
  "disabled_at": "2026-09-30T12:05:00Z"
}
```

**Never print channel config to debug** — it contains secrets (webhook URLs, bot tokens, SMTP passwords). The attempt record and health fields have everything you need.

---

## Retry a Failed Delivery

After fixing the root cause (e.g., rotated webhook URL), re-send the alert:

```
POST /api/v1/alerts/{alert_id}/deliveries/{channel_id}/retry
```

### When Retry Is Safe
- ✅ Fixed invalid credential (rotated webhook/token)
- ✅ Target service recovered from outage
- ✅ Network issue resolved

### When Retry Is Unsafe
- ❌ Alert was suppressed by cooldown (would create duplicate)
- ❌ Alert was duplicate (dedup already applied)
- ❌ You haven't fixed the root cause (will fail again)

The retry endpoint creates a **new delivery attempt** for the same alert/channel pair. It does not re-evaluate rules or create new alerts.

---

## API Reference

| Endpoint | Purpose |
|----------|---------|
| `GET /api/v1/alerts/{id}/deliveries` | List all delivery attempts for an alert |
| `POST /api/v1/alerts/{id}/deliveries/{channel_id}/retry` | Retry one failed delivery |
| `GET /api/v1/channels` | List channels with health counters |
| `GET /api/v1/monitors/{id}` | Verify monitor has channels attached |

---

## Dashboard Reference

| Page | What to Check |
|------|---------------|
| **Alert Detail** | Delivery attempts table with status, response code, snippet |
| **Channel List** | Health counters, `disabled_at`, last error |
| **Monitor Detail** | Attached channels, last matched time |

---

## Prevention

- Set `CHANNEL_DISABLE_AFTER_FAILURES` to auto-disable broken channels
- Use **digest mode** for high-volume channels to avoid rate limits
- Configure **maintenance windows** to silence expected downtime
- Monitor `/health` and `/readyz` endpoints for poller health
- Rotate credentials proactively before they expire

---

## Summary Checklist

- [ ] Alert exists in `/api/v1/alerts?monitor_id={id}`
- [ ] Delivery attempt exists in `/api/v1/alerts/{id}/deliveries`
- [ ] `response_code` indicates the failure type
- [ ] Fix root cause (rotate credential, recreate webhook, etc.)
- [ ] Channel health shows `enabled: true` (or re-enable via PATCH)
- [ ] Retry delivery via `POST /api/v1/alerts/{id}/deliveries/{channel_id}/retry`
- [ ] Verify new attempt shows `status: sent` with 2xx response