package store

import (
	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/domain"
)

// NoticesIn returns the [domain.NoticeWriter] that writes notices through app: pass the transaction's app (tx) that
// makes the change the notice reports, so that both are committed together. See [domain.Notify].
func NoticesIn(app core.App) domain.NoticeWriter {
	return noticeWriter{app: app}
}

type noticeWriter struct{ app core.App }

// WriteNotice implements domain.NoticeWriter: one unread row in notices (its uuid is set by the store hooks).
func (w noticeWriter) WriteNotice(n domain.Notice) error {
	c, err := w.app.FindCachedCollectionByNameOrId(Notices)
	if err != nil {
		return err
	}
	r := core.NewRecord(c)
	r.Set("member", n.MemberID)
	r.Set("kind", n.Kind)
	r.Set("ref", n.RefUUID)
	return w.app.Save(r)
}
