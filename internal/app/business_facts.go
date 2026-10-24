package app

import (
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"licentivo-launch/internal/storage"
	"time"
)

type identityTarget struct {
	ConnectionID string `json:"connection_id"`
	Environment  string `json:"environment"`
}

type identityFact struct {
	Schema     string           `json:"schema"`
	EventID    string           `json:"event_id"`
	AccountRef string           `json:"account_ref"`
	Sequence   uint64           `json:"sequence"`
	Type       string           `json:"type"`
	OccurredAt time.Time        `json:"occurred_at"`
	Data       json.RawMessage  `json:"data"`
	Targets    []identityTarget `json:"targets"`
}

// Freeze recipients in the same transaction as local identity invalidation.
// Connections created later must never receive a historical account cutoff.
func (s *Service) queueBusinessFact(tx *gorm.DB, userID, kind, key string, at time.Time, data any) error {
	var existing storage.LaunchOutbox
	if err := tx.First(&existing, "fact_key = ?", key).Error; err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	subject := s.subject(userID)
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&storage.LaunchEventStream{Subject: subject}).Error; err != nil {
		return err
	}
	var stream storage.LaunchEventStream
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&stream, "subject = ?", subject).Error; err != nil {
		return err
	}
	stream.Sequence++
	if err := tx.Model(&stream).Update("sequence", stream.Sequence).Error; err != nil {
		return err
	}
	id, err := newID()
	if err != nil {
		return err
	}
	var connections []storage.LaunchTrustConnection
	if err := tx.Order("environment").Find(&connections).Error; err != nil {
		return err
	}
	targets := make([]identityTarget, 0, len(connections))
	for _, connection := range connections {
		targets = append(targets, identityTarget{ConnectionID: connection.RemoteID, Environment: connection.Environment})
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	body, err := json.Marshal(identityFact{Schema: "launch-local-fact-v1", EventID: id, AccountRef: subject, Sequence: stream.Sequence, Type: kind, OccurredAt: at, Data: encoded, Targets: targets})
	if err != nil {
		return err
	}
	status := "pending"
	var delivered *time.Time
	if len(targets) == 0 {
		status = "not_required"
		delivered = &at
	}
	return tx.Create(&storage.LaunchOutbox{ID: id, FactKey: key, Subject: subject, Sequence: stream.Sequence, Kind: kind, Body: body, Status: status, DeliveredAt: delivered, AvailableAt: now(), CreatedAt: now()}).Error
}
