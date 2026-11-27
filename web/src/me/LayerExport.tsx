import { useTranslation } from 'react-i18next'
import { ComingSoonSection } from '@/components/ComingSoon'

/** The GeoJSON downloads of the places and recommendations layers on the "me" page. Placeholder until T041. */
export function LayerExport() {
  const { t } = useTranslation('layerExport')
  return <ComingSoonSection title={t('title')} />
}
