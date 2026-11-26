import { useCallback, useEffect, useState } from 'react'

export interface Position {
  lon: number
  lat: number
}

export type LocateMessage = 'unsupported' | 'denied' | 'failed' | 'outside'

const messageMillis = 5000

/**
 * "Show my location" (A8, E4). The position comes from the browser's geolocation and stays in the page: it is handed
 * to onPosition and never sent to the server or stored. onPosition returns false when the position lies outside every
 * city. A message, when there is one, disappears after a few seconds.
 */
export function useLocate(onPosition: (position: Position) => boolean) {
  const [locating, setLocating] = useState(false)
  const [message, setMessage] = useState<LocateMessage | null>(null)

  useEffect(() => {
    if (!message) {
      return
    }
    const timer = setTimeout(() => {
      setMessage(null)
    }, messageMillis)
    return () => {
      clearTimeout(timer)
    }
  }, [message])

  const locate = useCallback(() => {
    if (!('geolocation' in navigator)) {
      setMessage('unsupported')
      return
    }
    setLocating(true)
    setMessage(null)
    navigator.geolocation.getCurrentPosition(
      ({ coords }) => {
        setLocating(false)
        if (!onPosition({ lon: coords.longitude, lat: coords.latitude })) {
          setMessage('outside')
        }
      },
      (error) => {
        setLocating(false)
        setMessage(error.code === error.PERMISSION_DENIED ? 'denied' : 'failed')
      },
      { enableHighAccuracy: true, timeout: 15000, maximumAge: 60000 },
    )
  }, [onPosition])

  return { locate, locating, message }
}
