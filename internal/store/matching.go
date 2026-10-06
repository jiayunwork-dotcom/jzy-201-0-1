package store

import (
	"context"
	"database/sql"

	"microwaveplanner/internal/model"
	"microwaveplanner/internal/params"
)

// MatchingEndpoints returns endpoints that actually resolve the given scope.
// Site-layer scope kinds are endpoint_radio and endpoint_antenna; both have the
// endpoint ID as scope ID.
func (s *Store) MatchingEndpoints(ctx context.Context, layer params.Layer, scopeKind, scopeID string) ([]model.Endpoint, error) {
	var rs *sql.Rows
	var err error
	const selectEndpoint = `SELECT id,site_id,name,radio_model_id,radio_batch_id,antenna_model_id,antenna_batch_id
				 FROM endpoints`
	switch {
	case layer == params.LayerNominal && scopeKind == "radio_model":
		rs, err = s.DB.QueryContext(ctx, selectEndpoint+` WHERE radio_model_id=? ORDER BY id`, scopeID)
	case layer == params.LayerNominal && scopeKind == "antenna_model":
		rs, err = s.DB.QueryContext(ctx, selectEndpoint+` WHERE antenna_model_id=? ORDER BY id`, scopeID)
	case layer == params.LayerBatch && scopeKind == "radio_batch":
		rs, err = s.DB.QueryContext(ctx, selectEndpoint+` WHERE radio_batch_id=? ORDER BY id`, scopeID)
	case layer == params.LayerBatch && scopeKind == "antenna_batch":
		rs, err = s.DB.QueryContext(ctx, selectEndpoint+` WHERE antenna_batch_id=? ORDER BY id`, scopeID)
	case layer == params.LayerSite && (scopeKind == "endpoint_radio" || scopeKind == "endpoint_antenna"):
		rs, err = s.DB.QueryContext(ctx, selectEndpoint+` WHERE id=? ORDER BY id`, scopeID)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer rs.Close()

	var out []model.Endpoint
	for rs.Next() {
		ep, err := scanEndpoint(rs)
		if err != nil {
			return nil, err
		}
		out = append(out, *ep)
	}
	return out, rs.Err()
}

// EffectiveHashes returns deterministic numeric hashes for supplied endpoints
// using current head versions.
func (s *Store) EffectiveHashes(ctx context.Context, endpointIDs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range endpointIDs {
		v, _, err := s.ResolveEndpoint(ctx, id)
		if err != nil {
			return nil, err
		}
		out[id] = v.NumericHash()
	}
	return out, nil
}
