package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/leehyowon14/KLAP-for-GPT/internal/state"
	"github.com/ory/fosite"
)

type transactionKey struct{}
type oauthStore struct{ db *state.Store }
type savedRequest struct {
	ID                                                               string
	RequestedAt                                                      time.Time
	ClientID                                                         string
	RequestedScope, GrantedScope, RequestedAudience, GrantedAudience fosite.Arguments
	Form                                                             url.Values
	Session                                                          fosite.DefaultSession
}
type tokenRecord struct {
	Request         savedRequest
	Active          bool
	AccessSignature string
}

func (s *oauthStore) records(ctx context.Context, write bool, fn func(state.Records) error) error {
	if r, ok := ctx.Value(transactionKey{}).(state.Records); ok {
		return fn(r)
	}
	if write {
		return s.db.Update(fn)
	}
	return s.db.View(fn)
}
func (s *oauthStore) GetClient(ctx context.Context, id string) (fosite.Client, error) {
	var c fosite.DefaultClient
	err := s.records(ctx, false, func(r state.Records) error {
		var e error
		c, e = state.Get[fosite.DefaultClient](r, "oauth/client/"+id)
		return e
	})
	if errors.Is(err, state.ErrNotFound) {
		return nil, fosite.ErrNotFound
	}
	return &c, err
}
func (s *oauthStore) ClientAssertionJWTValid(context.Context, string) error {
	return fosite.ErrRequestUnauthorized
}
func (s *oauthStore) SetClientAssertionJWT(context.Context, string, time.Time) error {
	return fosite.ErrRequestUnauthorized
}

func saveRequest(r fosite.Requester) (savedRequest, error) {
	v := savedRequest{ID: r.GetID(), RequestedAt: r.GetRequestedAt(), ClientID: r.GetClient().GetID(), RequestedScope: r.GetRequestedScopes(), GrantedScope: r.GetGrantedScopes(), RequestedAudience: r.GetRequestedAudience(), GrantedAudience: r.GetGrantedAudience(), Form: url.Values{}}
	for _, key := range []string{"redirect_uri", "code_challenge", "code_challenge_method", "resource", "client_id"} {
		if values := r.GetRequestForm()[key]; values != nil {
			v.Form[key] = append([]string{}, values...)
		}
	}
	body, err := json.Marshal(r.GetSession())
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(body, &v.Session)
	return v, err
}
func (s *oauthStore) hydrate(ctx context.Context, v savedRequest) (fosite.Requester, error) {
	c, err := s.GetClient(ctx, v.ClientID)
	if err != nil {
		return nil, err
	}
	return &fosite.Request{ID: v.ID, RequestedAt: v.RequestedAt, Client: c, RequestedScope: v.RequestedScope, GrantedScope: v.GrantedScope, RequestedAudience: v.RequestedAudience, GrantedAudience: v.GrantedAudience, Form: v.Form, Session: &v.Session}, nil
}
func (s *oauthStore) create(ctx context.Context, kind, id, access string, r fosite.Requester) error {
	v, err := saveRequest(r)
	if err != nil {
		return err
	}
	return s.records(ctx, true, func(records state.Records) error {
		return state.Put(records, "oauth/"+kind+"/"+id, tokenRecord{Request: v, Active: true, AccessSignature: access})
	})
}
func (s *oauthStore) get(ctx context.Context, kind, id string) (fosite.Requester, bool, error) {
	var v tokenRecord
	err := s.records(ctx, false, func(r state.Records) error {
		var e error
		v, e = state.Get[tokenRecord](r, "oauth/"+kind+"/"+id)
		return e
	})
	if errors.Is(err, state.ErrNotFound) {
		return nil, false, fosite.ErrNotFound
	}
	if err != nil {
		return nil, false, err
	}
	req, err := s.hydrate(ctx, v.Request)
	return req, v.Active, err
}
func (s *oauthStore) remove(ctx context.Context, kind, id string) error {
	return s.records(ctx, true, func(r state.Records) error { delete(r, "oauth/"+kind+"/"+id); return nil })
}
func (s *oauthStore) CreateAuthorizeCodeSession(c context.Context, id string, r fosite.Requester) error {
	return s.create(c, "code", id, "", r)
}
func (s *oauthStore) GetAuthorizeCodeSession(c context.Context, id string, _ fosite.Session) (fosite.Requester, error) {
	r, active, e := s.get(c, "code", id)
	if e == nil && !active {
		e = fosite.ErrInvalidatedAuthorizeCode
	}
	return r, e
}
func (s *oauthStore) InvalidateAuthorizeCodeSession(c context.Context, id string) error {
	return s.records(c, true, func(r state.Records) error {
		v, e := state.Get[tokenRecord](r, "oauth/code/"+id)
		if e != nil {
			return fosite.ErrNotFound
		}
		v.Active = false
		return state.Put(r, "oauth/code/"+id, v)
	})
}
func (s *oauthStore) CreateAccessTokenSession(c context.Context, id string, r fosite.Requester) error {
	return s.create(c, "access", id, "", r)
}
func (s *oauthStore) GetAccessTokenSession(c context.Context, id string, _ fosite.Session) (fosite.Requester, error) {
	r, active, e := s.get(c, "access", id)
	if e == nil && !active {
		e = fosite.ErrInactiveToken
	}
	return r, e
}
func (s *oauthStore) DeleteAccessTokenSession(c context.Context, id string) error {
	return s.remove(c, "access", id)
}
func (s *oauthStore) CreateRefreshTokenSession(c context.Context, id, access string, r fosite.Requester) error {
	return s.create(c, "refresh", id, access, r)
}
func (s *oauthStore) GetRefreshTokenSession(c context.Context, id string, _ fosite.Session) (fosite.Requester, error) {
	r, active, e := s.get(c, "refresh", id)
	if e == nil && !active {
		e = fosite.ErrInactiveToken
	}
	return r, e
}
func (s *oauthStore) DeleteRefreshTokenSession(c context.Context, id string) error {
	return s.remove(c, "refresh", id)
}
func (s *oauthStore) CreatePKCERequestSession(c context.Context, id string, r fosite.Requester) error {
	return s.create(c, "pkce", id, "", r)
}
func (s *oauthStore) GetPKCERequestSession(c context.Context, id string, _ fosite.Session) (fosite.Requester, error) {
	r, _, e := s.get(c, "pkce", id)
	return r, e
}
func (s *oauthStore) DeletePKCERequestSession(c context.Context, id string) error {
	return s.remove(c, "pkce", id)
}
func (s *oauthStore) revoke(c context.Context, kind, requestID string, disableGrant bool) error {
	return s.records(c, true, func(records state.Records) error {
		for key := range records {
			if !strings.HasPrefix(key, "oauth/"+kind+"/") {
				continue
			}
			v, e := state.Get[tokenRecord](records, key)
			if e != nil {
				return e
			}
			if v.Request.ID != requestID {
				continue
			}
			v.Active = false
			if e = state.Put(records, key, v); e != nil {
				return e
			}
			if disableGrant {
				delete(records, "oauth/grant/"+v.Request.ClientID)
			}
		}
		return nil
	})
}
func (s *oauthStore) RevokeRefreshToken(c context.Context, id string) error {
	return s.revoke(c, "refresh", id, true)
}
func (s *oauthStore) RevokeAccessToken(c context.Context, id string) error {
	return s.revoke(c, "access", id, true)
}
func (s *oauthStore) RotateRefreshToken(c context.Context, id, signature string) error {
	// Rotation keeps the user grant; replay-triggered revocation removes it.
	if err := s.revoke(c, "refresh", id, false); err != nil {
		return err
	}
	return s.revoke(c, "access", id, false)
}
