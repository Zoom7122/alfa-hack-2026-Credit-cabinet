package decision

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/runtimehash"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Persistent struct {
	Core *Service
	Pool *pgxpool.Pool
}

func (s *Persistent) ModelVersion(r contracts.DecisionRequest) (string, error) {
	return s.Core.ModelVersion(r)
}
func (s *Persistent) Decide(ctx context.Context, r contracts.DecisionRequest) (contracts.DecisionResponse, error) {
	return s.Core.Decide(ctx, r)
}
func (s *Persistent) ModelSnapshot(ctx context.Context, r contracts.DecisionRequest) (context.Context, modelbindings.Metadata, error) {
	return s.Core.ModelSnapshot(ctx, r)
}
func (s *Persistent) DecideWithKey(ctx context.Context, r contracts.DecisionRequest, key string) (contracts.DecisionResponse, error) {
	out, _, err := s.DecideWithKeyModel(ctx, r, key)
	return out, err
}

// DecideWithKeyModel returns the metadata pinned before preparation, including on replay.
func (s *Persistent) DecideWithKeyModel(ctx context.Context, r contracts.DecisionRequest, key string) (contracts.DecisionResponse, modelbindings.Metadata, error) {
	var metadata modelbindings.Metadata
	empty := contracts.DecisionResponse{}
	body, err := json.Marshal(r)
	if err != nil {
		return empty, metadata, err
	}
	hash, err := runtimehash.Digest(body)
	if err != nil {
		return empty, metadata, err
	}
	var b [16]byte
	if _, err = rand.Read(b[:]); err != nil {
		return empty, metadata, err
	}
	token := hex.EncodeToString(b[:])
	var prepared, response []byte
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO credit_engine_runtime.scoring_runs(idempotency_key,request_hash) VALUES($1,$2) ON CONFLICT DO NOTHING`, key, hash); err != nil {
			return err
		}
		var oldHash string
		var active bool
		if err := tx.QueryRow(ctx, `SELECT request_hash,prepared,response,coalesce(lease_until>now(),false) FROM credit_engine_runtime.scoring_runs WHERE idempotency_key=$1 FOR UPDATE`, key).Scan(&oldHash, &prepared, &response, &active); err != nil {
			return err
		}
		if oldHash != hash {
			return &scoring.Error{Status: 409, Code: "IDEMPOTENCY_CONFLICT"}
		}
		if response != nil {
			return nil
		}
		if active {
			return &scoring.Error{Status: 409, Code: "OPERATION_IN_PROGRESS"}
		}
		_, err := tx.Exec(ctx, `UPDATE credit_engine_runtime.scoring_runs SET owner_token=$2,lease_until=now()+interval '60 seconds' WHERE idempotency_key=$1`, key, token)
		return err
	})
	if err != nil {
		return empty, metadata, err
	}
	if response != nil {
		var cached Prepared
		if len(prepared) > 0 {
			if err = json.Unmarshal(prepared, &cached); err != nil {
				return empty, metadata, err
			}
			if cached.Model != nil {
				metadata = *cached.Model
			}
		}
		err = json.Unmarshal(response, &empty)
		return empty, metadata, err
	}
	// A canceled HTTP request must still release its own lease, never another owner's.
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.Pool.Exec(c, `UPDATE credit_engine_runtime.scoring_runs SET owner_token=NULL,lease_until=NULL WHERE idempotency_key=$1 AND owner_token=$2`, key, token)
	}()
	var p Prepared
	if prepared == nil {
		ctx, metadata, err = s.Core.ModelSnapshot(ctx, r)
		if err != nil {
			return empty, metadata, err
		}
		p, err = s.Core.Prepare(ctx, r, &key)
		if err != nil {
			return empty, metadata, err
		}
		p.Model = &metadata
		prepared, err = json.Marshal(p)
		if err != nil {
			return empty, metadata, err
		}
		tag, err := s.Pool.Exec(ctx, `UPDATE credit_engine_runtime.scoring_runs SET prepared=$3 WHERE idempotency_key=$1 AND owner_token=$2 AND lease_until>now()`, key, token, prepared)
		if err != nil {
			return empty, metadata, err
		}
		if tag.RowsAffected() != 1 {
			return empty, metadata, &scoring.Error{Status: 409, Code: "OPERATION_IN_PROGRESS"}
		}
	} else if err = json.Unmarshal(prepared, &p); err != nil {
		return empty, metadata, err
	}
	if p.Model != nil {
		metadata = *p.Model
	}
	out, err := s.Core.Finish(ctx, p)
	if err != nil {
		return empty, metadata, err
	}
	response, err = json.Marshal(out)
	if err != nil {
		return empty, metadata, err
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE credit_engine_runtime.scoring_runs SET response=$3 WHERE idempotency_key=$1 AND owner_token=$2 AND lease_until>now()`, key, token, response)
	if err != nil {
		return empty, metadata, err
	}
	if tag.RowsAffected() != 1 {
		return empty, metadata, fmt.Errorf("scoring lease expired before result commit")
	}
	return out, metadata, nil
}
