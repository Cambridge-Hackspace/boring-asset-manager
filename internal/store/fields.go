package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/google/uuid"
)

var (
	ErrFieldNotFound  = errors.New("field not found")
	ErrFieldKeyExists = errors.New("field with this key already exists")
)

// CreateField validates and creates a new global field definition.
func (s *Store) CreateField(key, label, validationType string, rawOptions json.RawMessage) (*models.Field, error) {
	if _, err := fields.ValidateFieldDefinition(key, label, validationType, rawOptions); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	fieldID := uuid.NewString()

	var optionsStr *string
	if len(rawOptions) > 0 && string(rawOptions) != "null" {
		str := string(rawOptions)
		optionsStr = &str
	}

	query := `INSERT INTO fields (id, field_key, label, validation_type, options, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := s.Exec(query, fieldID, key, label, validationType, optionsStr, now, now)
	if err != nil {
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "unique") || strings.Contains(errMsg, "duplicate") {
			return nil, ErrFieldKeyExists
		}
		return nil, fmt.Errorf("inserting field: %w", err)
	}

	return &models.Field{
		ID:             fieldID,
		Key:            key,
		Label:          label,
		ValidationType: validationType,
		Options:        rawOptions,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// GetFieldByID retrieves a field definition by its UUID.
func (s *Store) GetFieldByID(id string) (*models.Field, error) {
	query := `SELECT id, field_key, label, validation_type, options, created_at, updated_at FROM fields WHERE id = ?`
	var (
		f          models.Field
		optionsVal sql.NullString
	)

	err := s.QueryRow(query, id).Scan(
		&f.ID, &f.Key, &f.Label, &f.ValidationType, &optionsVal, &f.CreatedAt, &f.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrFieldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying field by id: %w", err)
	}

	if optionsVal.Valid && optionsVal.String != "" {
		f.Options = json.RawMessage(optionsVal.String)
	}

	return &f, nil
}

// GetFieldByKey retrieves a field definition by its unique snake_case key.
func (s *Store) GetFieldByKey(key string) (*models.Field, error) {
	query := `SELECT id, field_key, label, validation_type, options, created_at, updated_at FROM fields WHERE field_key = ?`
	var (
		f          models.Field
		optionsVal sql.NullString
	)

	err := s.QueryRow(query, key).Scan(
		&f.ID, &f.Key, &f.Label, &f.ValidationType, &optionsVal, &f.CreatedAt, &f.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrFieldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying field by key: %w", err)
	}

	if optionsVal.Valid && optionsVal.String != "" {
		f.Options = json.RawMessage(optionsVal.String)
	}

	return &f, nil
}

// ListFields retrieves all registered field definitions ordered by key.
func (s *Store) ListFields() ([]models.Field, error) {
	query := `SELECT id, field_key, label, validation_type, options, created_at, updated_at FROM fields ORDER BY field_key ASC`
	rows, err := s.Query(query)
	if err != nil {
		return nil, fmt.Errorf("querying fields list: %w", err)
	}
	defer rows.Close()

	var result []models.Field
	for rows.Next() {
		var (
			f          models.Field
			optionsVal sql.NullString
		)
		if err := rows.Scan(
			&f.ID, &f.Key, &f.Label, &f.ValidationType, &optionsVal, &f.CreatedAt, &f.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning field row: %w", err)
		}
		if optionsVal.Valid && optionsVal.String != "" {
			f.Options = json.RawMessage(optionsVal.String)
		}
		result = append(result, f)
	}

	return result, nil
}

// UpdateField updates a field's label, validation type, and options schema.
func (s *Store) UpdateField(id, label, validationType string, rawOptions json.RawMessage) (*models.Field, error) {
	existing, err := s.GetFieldByID(id)
	if err != nil {
		return nil, err
	}

	if _, err := fields.ValidateFieldDefinition(existing.Key, label, validationType, rawOptions); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var optionsStr *string
	if len(rawOptions) > 0 && string(rawOptions) != "null" {
		str := string(rawOptions)
		optionsStr = &str
	}

	query := `UPDATE fields SET label = ?, validation_type = ?, options = ?, updated_at = ? WHERE id = ?`
	_, err = s.Exec(query, label, validationType, optionsStr, now, id)
	if err != nil {
		return nil, fmt.Errorf("updating field: %w", err)
	}

	existing.Label = label
	existing.ValidationType = validationType
	existing.Options = rawOptions
	existing.UpdatedAt = now

	return existing, nil
}

// DeleteField permanently deletes a field definition.
func (s *Store) DeleteField(id string) error {
	_, err := s.GetFieldByID(id)
	if err != nil {
		return err
	}

	query := `DELETE FROM fields WHERE id = ?`
	_, err = s.Exec(query, id)
	if err != nil {
		return fmt.Errorf("deleting field: %w", err)
	}
	return nil
}
