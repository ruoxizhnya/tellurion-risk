package pgstore

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"tellurion-risk/internal/rule"
)

func TestMapPgError_TranslatesUniqueViolations(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		want       error
		wantPrefix bool
	}{
		{"nil", nil, nil, false},
		{"rule defs pkey", &pgconn.PgError{Code: "23505", ConstraintName: "rule_defs_pkey"}, rule.ErrIdempotencyConflict, true},
		{"revision pkey", &pgconn.PgError{Code: "23505", ConstraintName: "rule_revisions_pkey"}, rule.ErrRevisionConflict, true},
		{"revision hash unique", &pgconn.PgError{Code: "23505", ConstraintName: "rule_revisions_rule_id_version_hash_key"}, rule.ErrRevisionConflict, true},
		{"unrelated unique", &pgconn.PgError{Code: "23505", ConstraintName: "other_idx"}, nil, false},
		{"foreign key", &pgconn.PgError{Code: "23503", ConstraintName: "rule_revisions_rule_id_fkey"}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapPgError(tc.err)
			if tc.err == nil {
				if got != nil {
					t.Fatalf("want nil, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("error must not be swallowed")
			}
			if errors.Is(got, tc.want) != tc.wantPrefix {
				t.Fatalf("got %v, want sentinel %v", got, tc.want)
			}
		})
	}
}
