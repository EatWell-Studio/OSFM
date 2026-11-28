// The pin of an exact place, drawn on a canvas in the page and handed to MapLibre as pixels: no image file to fetch and
// nothing from another origin (docs/architecture.md §4.3, §4.7). The basemap's sprite has no pin of this kind.

import type { Map as MapLibreMap } from 'maplibre-gl'
import { pinImageId } from './layers'

/** The pin's size in CSS pixels; its tip is at the bottom center. */
const width = 22
const height = 30

/** Draws the pin at the given pixel ratio; null where the browser has no 2D canvas. */
function drawPin(ratio: number): ImageData | null {
  const canvas = document.createElement('canvas')
  canvas.width = width * ratio
  canvas.height = height * ratio
  const ctx = canvas.getContext('2d')
  if (!ctx) {
    return null
  }
  ctx.scale(ratio, ratio)
  const cx = width / 2
  const r = cx - 1.5
  const cy = r + 1.5
  ctx.beginPath()
  ctx.moveTo(cx, height - 1)
  ctx.bezierCurveTo(cx - 2, height - 8, 1.5, cy + 6, 1.5, cy)
  ctx.arc(cx, cy, r, Math.PI, 2 * Math.PI)
  ctx.bezierCurveTo(width - 1.5, cy + 6, cx + 2, height - 8, cx, height - 1)
  ctx.closePath()
  ctx.fillStyle = '#dc2626'
  ctx.fill()
  ctx.lineWidth = 1.5
  ctx.strokeStyle = '#ffffff'
  ctx.stroke()
  ctx.beginPath()
  ctx.arc(cx, cy, 3.5, 0, 2 * Math.PI)
  ctx.fillStyle = '#ffffff'
  ctx.fill()
  return ctx.getImageData(0, 0, canvas.width, canvas.height)
}

/** Adds the pin to the map's style unless it is there already (a new style starts without it). */
export function addPinImage(map: MapLibreMap) {
  if (map.hasImage(pinImageId)) {
    return
  }
  const ratio = Math.max(2, Math.ceil(window.devicePixelRatio || 1))
  const image = drawPin(ratio)
  if (image) {
    map.addImage(pinImageId, image, { pixelRatio: ratio })
  }
}
