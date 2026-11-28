import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { BackupKindOSFM, CodeBackupRunning, CodeBackupsUnavailable } from '@/api/types.gen'
import { Button } from '@/components/ui/button'
import { credentialsQueryKey, errorCode, fetchCredentials } from '@/credentials/api'
import { useStepUp } from '@/credentials/useStepUp'
import { rateLimitedCode } from '@/lib/authApi'
import { saveFile } from '@/me/layerExportApi'
import {
  type BackupInfo,
  backupsQueryKey,
  createBackup,
  deleteBackup,
  downloadBackup,
  fetchBackups,
  formatSize,
} from './backupApi'

/**
 * Backups, /admin/backup (maintainers only; G5): what a backup holds and why it must be kept like the keys (it contains
 * the master key), a button that writes one now, and the backups on the server, newest first, each to download or
 * delete. Writing, downloading and deleting need a recent re-verification, as the server requires. Restoring is not
 * here: it needs the instance stopped and is done on the server with `osfm restore`.
 */
export function BackupAdmin() {
  const { t, i18n } = useTranslation(['backup', 'common'])
  const headingId = useId()
  const listId = useId()
  const queryClient = useQueryClient()
  const backups = useQuery({ queryKey: backupsQueryKey, queryFn: fetchBackups })
  const credentials = useQuery({ queryKey: credentialsQueryKey, queryFn: fetchCredentials })
  const stepUp = useStepUp(credentials.data)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState<string | null>(null)
  const [confirming, setConfirming] = useState<string | null>(null)

  const message = (err: unknown): string => {
    switch (errorCode(err)) {
      case CodeBackupRunning:
        return t('errors.running')
      case CodeBackupsUnavailable:
        return t('errors.unavailable')
      case rateLimitedCode:
        return t('errors.rateLimited')
      case 'not_found':
        void queryClient.invalidateQueries({ queryKey: backupsQueryKey })
        return t('errors.notFound')
      default:
        return t('errors.generic')
    }
  }
  const failed = (err: unknown) => {
    if (!stepUp.handleError(err)) {
      setError(message(err))
    }
  }
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: backupsQueryKey })
  }

  const create = useMutation({
    mutationFn: createBackup,
    onSuccess: (b) => {
      setDone(t('created', { name: b.name }))
    },
    onError: failed,
    onSettled: refresh,
  })
  const download = useMutation({
    mutationFn: async (name: string) => {
      saveFile(await downloadBackup(name), name)
      return name
    },
    onSuccess: (name) => {
      setDone(t('downloaded', { name }))
    },
    onError: failed,
  })
  const remove = useMutation({
    mutationFn: deleteBackup,
    onSuccess: (_, name) => {
      setConfirming(null)
      setDone(t('deleted', { name }))
    },
    onError: failed,
    onSettled: refresh,
  })
  const start = (action: () => void) => {
    setError(null)
    setDone(null)
    stepUp.guard(action)
  }
  const busy = create.isPending || download.isPending || remove.isPending
  const when = (iso: string) =>
    new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso))

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4 px-4">
      <h2 id={headingId} className="text-lg font-medium">
        {t('title')}
      </h2>
      <p className="text-sm text-muted-foreground">{t('intro')}</p>
      <p className="rounded-lg border border-destructive/40 p-3 text-sm">{t('secret')}</p>
      <p className="text-sm text-muted-foreground">{t('restore')}</p>
      {stepUp.panel}
      <Button
        className="self-start"
        disabled={busy}
        onClick={() => {
          start(() => {
            create.mutate()
          })
        }}
      >
        {create.isPending ? t('creating') : t('create')}
      </Button>
      {error !== null && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {done !== null && (
        <p role="status" className="text-sm">
          {done}
        </p>
      )}
      <h3 id={listId} className="font-medium">
        {t('list')}
      </h3>
      {backups.isPending && (
        <p role="status" className="text-muted-foreground">
          {t('common:app.loading')}
        </p>
      )}
      {backups.isError && (
        <p role="alert" className="text-sm text-destructive">
          {t('errors.load')}
        </p>
      )}
      {backups.data?.length === 0 && <p className="text-sm text-muted-foreground">{t('empty')}</p>}
      {backups.data && backups.data.length > 0 && (
        <ul aria-labelledby={listId} className="flex flex-col gap-3">
          {backups.data.map((b: BackupInfo) => (
            <li key={b.name} aria-label={b.name} className="flex flex-col gap-2 rounded-lg border p-3">
              <p className="font-mono text-sm break-all">{b.name}</p>
              <p className="text-xs text-muted-foreground">
                {t(b.kind === BackupKindOSFM ? 'kind.osfm' : 'kind.premigrate')} · {when(b.createdAt)} ·{' '}
                {formatSize(b.size, i18n.language)}
              </p>
              {confirming === b.name ? (
                <div className="flex flex-col gap-2">
                  <p className="text-sm">{t('confirmDelete', { name: b.name })}</p>
                  <div className="flex flex-wrap gap-2">
                    <Button
                      variant="destructive"
                      disabled={busy}
                      onClick={() => {
                        start(() => {
                          remove.mutate(b.name)
                        })
                      }}
                    >
                      {t('deleteConfirm')}
                    </Button>
                    <Button
                      variant="ghost"
                      onClick={() => {
                        setConfirming(null)
                      }}
                    >
                      {t('cancel')}
                    </Button>
                  </div>
                </div>
              ) : (
                <div className="flex flex-wrap gap-2">
                  <Button
                    variant="outline"
                    disabled={busy}
                    onClick={() => {
                      start(() => {
                        download.mutate(b.name)
                      })
                    }}
                  >
                    {download.isPending && download.variables === b.name ? t('downloading') : t('download')}
                  </Button>
                  <Button
                    variant="ghost"
                    disabled={busy}
                    onClick={() => {
                      setError(null)
                      setDone(null)
                      setConfirming(b.name)
                    }}
                  >
                    {t('delete')}
                  </Button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
