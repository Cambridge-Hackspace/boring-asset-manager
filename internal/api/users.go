package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// CreateUserRequest defines the request body for creating a user account.
type CreateUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	IsActive *bool  `json:"is_active,omitempty"`
}

// UpdateUserStatusRequest defines the request body for modifying user status.
type UpdateUserStatusRequest struct {
	IsActive bool `json:"is_active"`
}

// handleListUsers lists all non-archived users.
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to list users")
		return
	}
	if users == nil {
		users = []models.User{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"users": users,
	})
}

// handleCreateUser provisions a new user account.
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		writeJSONError(w, http.StatusBadRequest, "Email and password are required")
		return
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	user, err := s.store.CreateUser(req.Email, req.Password, isActive)
	if err != nil {
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "unique") || strings.Contains(errMsg, "duplicate") {
			writeJSONError(w, http.StatusConflict, "A user with this email already exists")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create user: %v", err))
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"user": user,
	})
}

// handleUpdateUserStatus toggles an active/disabled user account with Anti-Lockout enforcement.
func (s *Server) handleUpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "User ID is required")
		return
	}

	var req UpdateUserStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	err := s.store.UpdateUserStatus(id, req.IsActive)
	if errors.Is(err, store.ErrUserNotFound) {
		writeJSONError(w, http.StatusNotFound, "User not found")
		return
	}
	if errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update user status: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "User status updated successfully",
	})
}

// handleDeleteUser archives/deletes a user account with Anti-Lockout enforcement.
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "User ID is required")
		return
	}

	err := s.store.DeleteUser(id)
	if errors.Is(err, store.ErrUserNotFound) {
		writeJSONError(w, http.StatusNotFound, "User not found")
		return
	}
	if errors.Is(err, rbac.ErrAntiLockoutTriggered) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete user: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "User deleted successfully",
	})
}
