package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Recovery codes (D3, N5): ten one-time codes a member receives once two-step verification is set up, for signing in
// when the passkey or the authenticator app is lost. Only their hashes are stored.

// RecoveryCodeCount is the number of recovery codes a member has.
const RecoveryCodeCount = 10

// recoveryCodeBytes is the randomness of a recovery code: 128 bits.
const recoveryCodeBytes = 16

// NewRecoveryCodes gives member a new set of [RecoveryCodeCount] recovery codes, deleting the ones they had, and
// returns them. The codes exist only in this return value: the recovery_codes collection keeps only their SHA-256
// hashes. Call it with the app of the transaction that sets up or resets the member's credentials.
//
// A code is 128 random bits as 32 hexadecimal digits in groups of four ("1f0c-…"); with that much randomness a plain
// hash is enough. [HashRecoveryCode] gives the stored form of a code as a member types it.
func NewRecoveryCodes(tx core.App, member *core.Record) ([]string, error) {
	if !isMember(member) {
		return nil, ErrNotMember
	}
	c, err := tx.FindCachedCollectionByNameOrId(store.RecoveryCodes)
	if err != nil {
		return nil, err
	}
	if _, err := tx.DB().Delete(store.RecoveryCodes, dbx.HashExp{"member": member.Id}).Execute(); err != nil {
		return nil, fmt.Errorf("auth: delete old recovery codes: %w", err)
	}
	codes := make([]string, 0, RecoveryCodeCount)
	for range RecoveryCodeCount {
		b := make([]byte, recoveryCodeBytes)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("auth: recovery code: %w", err)
		}
		h := hex.EncodeToString(b)
		var groups []string
		for i := 0; i < len(h); i += 4 {
			groups = append(groups, h[i:i+4])
		}
		code := strings.Join(groups, "-")
		r := core.NewRecord(c)
		r.Set("member", member.Id)
		r.Set("code_hash", HashRecoveryCode(code))
		if err := tx.Save(r); err != nil {
			return nil, fmt.Errorf("auth: save recovery code: %w", err)
		}
		codes = append(codes, code)
	}
	return codes, nil
}

// HashRecoveryCode returns the stored form of a recovery code: the hex SHA-256 of the code in lower case without
// spaces and dashes, so that a code typed in another case or grouping matches.
func HashRecoveryCode(code string) string {
	normalized := strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, strings.ToLower(strings.TrimSpace(code)))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}
