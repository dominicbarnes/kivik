// Licensed under the Apache License, Version 2.0 (the "License"); you may not
// use this file except in compliance with the License. You may obtain a copy of
// the License at
//
//  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations under
// the License.

//go:build !js
// +build !js

package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"gitlab.com/flimzy/httpe"

	"github.com/go-kivik/kivik/v4"
	internal "github.com/go-kivik/kivik/v4/int/errors"
	"github.com/go-kivik/kivik/v4/x/server/auth"
)

type contextKey struct{ name string }

var userContextKey = &contextKey{"userCtx"}

func userFromContext(ctx context.Context) *auth.UserContext {
	user, _ := ctx.Value(userContextKey).(*auth.UserContext)
	return user
}

type authService struct {
	s *Server
}

var _ auth.Server = (*authService)(nil)

// UserStore returns the aggregate UserStore for the server.
func (s *authService) UserStore() auth.UserStore {
	return s.s.userStores
}

func (s *authService) Bind(r *http.Request, v interface{}) error {
	return s.s.bind(r, v)
}

type doneWriter struct {
	http.ResponseWriter
	done bool
}

func (w *doneWriter) WriteHeader(status int) {
	w.done = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *doneWriter) Write(b []byte) (int, error) {
	w.done = true
	return w.ResponseWriter.Write(b)
}

// authMiddleware sets the user context based on the authenticated user, if any.
func (s *Server) authMiddleware(next httpe.HandlerWithError) httpe.HandlerWithError {
	return httpe.HandlerWithErrorFunc(func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		if len(s.authFuncs) == 0 {
			// Admin party!
			r = r.WithContext(context.WithValue(ctx, userContextKey, &auth.UserContext{
				Name:  "admin",
				Roles: []string{auth.RoleAdmin},
			}))
			return next.ServeHTTPWithError(w, r)
		}

		dw := &doneWriter{ResponseWriter: w}

		var userCtx *auth.UserContext
		var err error
		for _, authFunc := range s.authFuncs {
			userCtx, err = authFunc(dw, r)
			if err != nil {
				return err
			}
			if dw.done {
				return nil
			}
			if userCtx != nil {
				break
			}
		}
		r = r.WithContext(context.WithValue(ctx, userContextKey, userCtx))
		return next.ServeHTTPWithError(w, r)
	})
}

// adminRequired returns Status Forbidden if the session is not authenticated as
// an admin.
func adminRequired(next httpe.HandlerWithError) httpe.HandlerWithError {
	return httpe.HandlerWithErrorFunc(func(w http.ResponseWriter, r *http.Request) error {
		userCtx, _ := r.Context().Value(userContextKey).(*auth.UserContext)
		if userCtx == nil {
			return &internal.Error{Status: http.StatusUnauthorized, Message: "User not authenticated"}
		}
		if !userCtx.HasRole(auth.RoleAdmin) {
			return &internal.Error{Status: http.StatusForbidden, Message: "Admin privileges required"}
		}
		return next.ServeHTTPWithError(w, r)
	})
}

func (s *Server) dbMembershipRequired(next httpe.HandlerWithError) httpe.HandlerWithError {
	return httpe.HandlerWithErrorFunc(func(w http.ResponseWriter, r *http.Request) error {
		db := chi.URLParam(r, "db")
		security, err := s.client.DB(db).Security(r.Context())
		if err != nil {
			return &internal.Error{Status: http.StatusBadGateway, Err: err}
		}

		if err := validateDBMembership(userFromContext(r.Context()), security); err != nil {
			return err
		}

		return next.ServeHTTPWithError(w, r)
	})
}

// validateDBMembership returns an error if the user lacks sufficient membership.
//
//	See the [CouchDB documentation] for the rules for granting access.
//
// [CouchDB documentation]: https://docs.couchdb.org/en/stable/api/database/security.html#get--db-_security
func validateDBMembership(user *auth.UserContext, security *kivik.Security) error {
	// No membership names/roles means open read access.
	if len(security.Members.Names) == 0 && len(security.Members.Roles) == 0 {
		return nil
	}

	if user == nil {
		return &internal.Error{Status: http.StatusUnauthorized, Message: "User not authenticated"}
	}

	for _, name := range security.Members.Names {
		if name == user.Name {
			return nil
		}
	}
	for _, role := range security.Members.Roles {
		if user.HasRole(role) {
			return nil
		}
	}
	for _, name := range security.Admins.Names {
		if name == user.Name {
			return nil
		}
	}
	for _, role := range security.Admins.Roles {
		if user.HasRole(role) {
			return nil
		}
	}
	if user.HasRole(auth.RoleAdmin) {
		return nil
	}
	return &internal.Error{Status: http.StatusForbidden, Message: "User lacks sufficient privileges"}
}

func (s *Server) dbAdminRequired(next httpe.HandlerWithError) httpe.HandlerWithError {
	return httpe.HandlerWithErrorFunc(func(w http.ResponseWriter, r *http.Request) error {
		db := chi.URLParam(r, "db")
		security, err := s.client.DB(db).Security(r.Context())
		if err != nil {
			return &internal.Error{Status: http.StatusBadGateway, Err: err}
		}

		if err := validateDBAdmin(userFromContext(r.Context()), security); err != nil {
			return err
		}

		return next.ServeHTTPWithError(w, r)
	})
}

// validateDBAdmin returns an error if the user lacks sufficient membership.
//
//	See the [CouchDB documentation] for the rules for granting access.
//
// [CouchDB documentation]: https://docs.couchdb.org/en/stable/api/database/security.html#get--db-_security
func validateDBAdmin(user *auth.UserContext, security *kivik.Security) error {
	if user == nil {
		return &internal.Error{Status: http.StatusUnauthorized, Message: "User not authenticated"}
	}
	for _, name := range security.Admins.Names {
		if name == user.Name {
			return nil
		}
	}
	if user.HasRole(auth.RoleAdmin) {
		return nil
	}
	for _, role := range security.Admins.Roles {
		if user.HasRole(role) {
			return nil
		}
	}
	return &internal.Error{Status: http.StatusForbidden, Message: "User lacks sufficient privileges"}
}
