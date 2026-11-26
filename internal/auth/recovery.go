package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/store"
)

// Recovery codes (D3, D4, N5). Every member gets [RecoveryCodeCount] one-time codes when registering, with a passkey
// or with password and TOTP; a code replaces the second factor once (password + recovery code) and is then used up.
//
//   - A code is 80 random bits from crypto/rand in Crockford's base32: 16 characters from 0–9 and A–Z without I, L, O
//     and U, shown in groups of four ("7K3D-QX9M-…"). When a code is entered, case, spaces and dashes do not matter,
//     and O, I and L are read as 0, 1 and 1.
//   - Only the SHA-256 hash of a code is stored (recovery_codes.code_hash, unique). With 80 random bits a plain hash
//     is enough: an offline search of 2^80 candidates is not feasible, and the database alone yields no usable code.
//   - A code is used with a conditional update (only while used_at is empty), so that two requests with the same code
//     cannot both succeed.
//   - New codes replace all of a member's previous codes, used or not.

// RecoveryCodeCount is the number of recovery codes a member gets.
const RecoveryCodeCount = 10

const (
	recoveryCodeBytes  = 10 // 80 bits, 16 base32 characters
	recoveryCodeLength = 16
	recoveryGroup      = 4
)

var crockford = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// NewRecoveryCodes generates [RecoveryCodeCount] new recovery codes for member, deletes the member's previous codes,
// stores only the hashes of the new ones and returns the codes in their display form ("XXXX-XXXX-XXXX-XXXX"). The
// codes exist only in this return value: show them to the member once.
//
// It runs in a transaction, or in the caller's when tx is a transaction's app (inside a [Registration], pass its tx).
func NewRecoveryCodes(tx core.App, member *core.Record) ([]string, error) {
	if !isMember(member) || member.Id == "" {
		return nil, ErrNotMember
	}
	codes := make([]string, 0, RecoveryCodeCount)
	hashes := make([]string, 0, RecoveryCodeCount)
	for range RecoveryCodeCount {
		b := make([]byte, recoveryCodeBytes)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("auth: recovery code: %w", err)
		}
		code := crockford.EncodeToString(b)
		codes = append(codes, displayRecoveryCode(code))
		hashes = append(hashes, hashRecoveryCode(code))
	}
	err := tx.RunInTransaction(func(tx core.App) error {
		if _, err := tx.DB().Delete(store.RecoveryCodes, dbx.HashExp{"member": member.Id}).Execute(); err != nil {
			return err
		}
		c, err := tx.FindCachedCollectionByNameOrId(store.RecoveryCodes)
		if err != nil {
			return err
		}
		for _, h := range hashes {
			r := core.NewRecord(c)
			r.Set("member", member.Id)
			r.Set("code_hash", h)
			if err := tx.Save(r); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("auth: save recovery codes: %w", err)
	}
	return codes, nil
}

// displayRecoveryCode groups a code in fours.
func displayRecoveryCode(code string) string {
	var b strings.Builder
	for i := 0; i < len(code); i += recoveryGroup {
		if i > 0 {
			b.WriteByte('-')
		}
		b.WriteString(code[i:min(i+recoveryGroup, len(code))])
	}
	return b.String()
}

// normalizeRecoveryCode returns a code as entered in its canonical form (16 characters of the alphabet), and whether
// it has the form of a code at all.
func normalizeRecoveryCode(input string) (string, bool) {
	var b strings.Builder
	for _, c := range strings.ToUpper(input) {
		switch {
		case c == '-' || c == ' ' || c == '\t':
			continue
		case c == 'O':
			c = '0'
		case c == 'I' || c == 'L':
			c = '1'
		case (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z' && c != 'U'):
		default:
			return "", false
		}
		b.WriteRune(c)
	}
	if b.Len() != recoveryCodeLength {
		return "", false
	}
	return b.String(), true
}

// hashRecoveryCode returns the stored form of a canonical code: hex SHA-256.
func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// useRecoveryCode uses up one of member's unused recovery codes if code is one of them, and reports whether it was.
// The conditional update lets only one of two concurrent requests with the same code succeed.
func useRecoveryCode(app core.App, member *core.Record, code string) (bool, error) {
	canonical, ok := normalizeRecoveryCode(code)
	if !ok {
		return false, nil
	}
	t := dbTime(now())
	res, err := app.DB().Update(store.RecoveryCodes, dbx.Params{"used_at": t, store.FieldUpdated: t},
		dbx.HashExp{"member": member.Id, "code_hash": hashRecoveryCode(canonical), "used_at": ""}).Execute()
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// RemainingRecoveryCodes returns the number of member's recovery codes that have not been used.
func RemainingRecoveryCodes(app core.App, member *core.Record) (int64, error) {
	var n int64
	err := app.DB().Select("COUNT(*)").From(store.RecoveryCodes).
		Where(dbx.HashExp{"member": member.Id, "used_at": ""}).Row(&n)
	return n, err
}
