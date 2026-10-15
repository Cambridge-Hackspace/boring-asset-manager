package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/auth"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// CreateUserRequest defines the request body for creating a user account.
type CreateUserRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
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
		writeServerError(w, err, "list users")
		return
	}
	roleIDs, err := s.store.UserRoleIDs()
	if err != nil {
		writeServerError(w, err, "list user roles")
		return
	}
	list := make([]userWithRoles, 0, len(users))
	for _, u := range users {
		ids := roleIDs[u.ID]
		if ids == nil {
			ids = []string{}
		}
		list = append(list, userWithRoles{User: u, RoleIDs: ids})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"users": list,
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

	user, err := s.store.CreateNamedUser(req.Email, req.Name, req.Password, isActive)
	if errors.Is(err, auth.ErrPasswordPolicy) || errors.Is(err, store.ErrInvalidUser) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if store.IsUniqueViolation(err) {
		writeJSONError(w, http.StatusConflict, "A user with this email already exists")
		return
	}
	if err != nil {
		writeStoreError(w, err, "create the user")
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
		writeStoreError(w, err, "update the user")
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
		writeStoreError(w, err, "delete the user")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "User deleted successfully",
	})
}

// userWithRoles is a user as the user list returns it.
type userWithRoles struct {
	models.User
	RoleIDs []string `json:"role_ids"`
}

// SetUserRolesRequest replaces a user's roles.
type SetUserRolesRequest struct {
	RoleIDs []string `json:"role_ids"`
}

// handleListRoles lists roles with their permissions: GET /api/v1/roles
func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := s.store.ListRoles()
	if err != nil {
		writeServerError(w, err, "list roles")
		return
	}
	if roles == nil {
		roles = []models.RoleWithPermissions{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

// handleSetUserRoles replaces a user's roles under the anti-lockout guard:
// PUT /api/v1/users/{id}/roles
func (s *Server) handleSetUserRoles(w http.ResponseWriter, r *http.Request) {
	var req SetUserRolesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RoleIDs == nil {
		writeJSONError(w, http.StatusBadRequest, "role_ids is required")
		return
	}
	err := s.store.SetUserRoles(r.PathValue("id"), req.RoleIDs)
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "User not found")
	case errors.Is(err, store.ErrRoleNotFound), errors.Is(err, rbac.ErrAntiLockoutTriggered):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeStoreError(w, err, "set the roles")
	default:
		ids, err := s.store.UserRoleIDs()
		if err != nil {
			writeServerError(w, err, "read roles")
			return
		}
		roleIDs := ids[r.PathValue("id")]
		if roleIDs == nil {
			roleIDs = []string{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"role_ids": roleIDs})
	}
}

// SetNameRequest sets a user's display name; empty shows their email.
type SetNameRequest struct {
	Name *string `json:"name"`
}

// handleSetUserName renames a user: PUT /api/v1/users/{id}/name
func (s *Server) handleSetUserName(w http.ResponseWriter, r *http.Request) {
	s.setUserName(w, r, r.PathValue("id"))
}

// handleSetOwnName renames the signed-in user: PUT /api/v1/auth/name
func (s *Server) handleSetOwnName(w http.ResponseWriter, r *http.Request) {
	s.setUserName(w, r, CurrentUser(r.Context()).ID)
}

func (s *Server) setUserName(w http.ResponseWriter, r *http.Request, userID string) {
	var req SetNameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == nil {
		writeJSONError(w, http.StatusBadRequest, "name is required (empty to show the email instead)")
		return
	}
	user, err := s.store.SetUserName(userID, *req.Name)
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "User not found")
	case errors.Is(err, store.ErrInvalidUser):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeStoreError(w, err, "rename the user")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"user": user})
	}
}
