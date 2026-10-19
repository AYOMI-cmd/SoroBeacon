package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/sorotrail/sorobeacon/internal/store"
)

func (s *Server) listDeadLetters(w http.ResponseWriter, r *http.Request) {
	var filter store.DeadLetterFilter
	if q := r.URL.Query().Get("channel_id"); q != "" {
		id, err := strconv.ParseInt(q, 10, 64)
		if err == nil {
			filter.ChannelID = id
		}
	}
	if q := r.URL.Query().Get("alert_id"); q != "" {
		id, err := strconv.ParseInt(q, 10, 64)
		if err == nil {
			filter.AlertID = id
		}
	}
	if q := r.URL.Query().Get("limit"); q != "" {
		limit, err := strconv.Atoi(q)
		if err == nil && limit > 0 {
			filter.Limit = limit
		}
	}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if q := r.URL.Query().Get("after_id"); q != "" {
		id, err := strconv.ParseInt(q, 10, 64)
		if err == nil {
			filter.AfterID = id
		}
	}

	letters, err := s.store.ListDeadLetters(r.Context(), filter)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if letters == nil {
		letters = []store.DeadLetter{}
	}
	writeJSON(w, http.StatusOK, letters)
}

func (s *Server) redriveDeadLetter(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, "malformed dead letter id")
		return
	}

	dl, err := s.store.GetDeadLetter(r.Context(), id)
	if err != nil {
		if errorsIsNotFound(err) {
			writeErr(w, r, http.StatusNotFound, "dead letter not found")
			return
		}
		s.fail(w, r, err)
		return
	}

	alert, err := s.store.GetAlert(r.Context(), dl.AlertID)
	if err != nil {
		if errorsIsNotFound(err) {
			writeErr(w, r, http.StatusNotFound, "alert not found")
			return
		}
		s.fail(w, r, err)
		return
	}

	ch, err := s.store.GetChannel(r.Context(), dl.ChannelID)
	if err != nil {
		if errorsIsNotFound(err) {
			writeErr(w, r, http.StatusNotFound, "channel not found")
			return
		}
		s.fail(w, r, err)
		return
	}

	attempts, err := s.store.ListDeliveryAttempts(r.Context(), alert.ID, "")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, att := range attempts {
		if att.ChannelID == ch.ID && att.Status == store.DeliveryStatusSuccess {
			writeRetryGate(w, r, errors.New("already succeeded"))
			return
		}
	}

	notifier, err := s.factory.New(ch.Type, ch.Config)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), ch.TimeoutDuration())
	defer cancel()

	notifyAlert := notifyAlertFromStore(ctx, s.store, *alert)
	err = notifier.Send(ctx, notifyAlert)
	attemptStatus := store.DeliveryStatusSuccess
	snippet := ""
	if err != nil {
		attemptStatus = store.DeliveryStatusFailed
		// Keep transport details out of the API response; notifier errors may
		// contain credentials or webhook URLs.
		snippet = "delivery failed"
	}

	_ = s.store.RecordDeliveryAttempt(ctx, &store.DeliveryAttempt{
		AlertID:         alert.ID,
		ChannelID:       ch.ID,
		Status:          attemptStatus,
		ResponseSnippet: snippet,
	})

	if err == nil {
		_ = s.store.DeleteDeadLetter(ctx, dl.ID)
		writeNoContent(w)
		return
	}

	writeErr(w, r, http.StatusBadGateway, "redrive delivery failed: "+snippet)
}

func errorsIsNotFound(err error) bool {
	return errors.Is(err, store.ErrNotFound)
}
