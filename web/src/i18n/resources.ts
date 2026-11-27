// The translations, one JSON file per namespace and language (locales/<language>/<namespace>.json; docs/architecture.md
// §4.1). A feature changes only its own namespace; every namespace of M2 is registered here already, so that adding
// texts never touches this file. zh-CN is the source locale: its files define the type of the others, so a key missing
// from en fails the type check (and locales.test.ts checks that both have the same keys and files).

import commonZhCN from './locales/zh-CN/common.json'
import authZhCN from './locales/zh-CN/auth.json'
import setupZhCN from './locales/zh-CN/setup.json'
import mapZhCN from './locales/zh-CN/map.json'
import placeZhCN from './locales/zh-CN/place.json'
import createZhCN from './locales/zh-CN/create.json'
import editZhCN from './locales/zh-CN/edit.json'
import shareZhCN from './locales/zh-CN/share.json'
import meZhCN from './locales/zh-CN/me.json'
import profileZhCN from './locales/zh-CN/profile.json'
import invitesZhCN from './locales/zh-CN/invites.json'
import noticesZhCN from './locales/zh-CN/notices.json'
import leaveZhCN from './locales/zh-CN/leave.json'
import personalExportZhCN from './locales/zh-CN/personalExport.json'
import layerExportZhCN from './locales/zh-CN/layerExport.json'
import adminZhCN from './locales/zh-CN/admin.json'
import membersZhCN from './locales/zh-CN/members.json'
import reportsZhCN from './locales/zh-CN/reports.json'
import backupZhCN from './locales/zh-CN/backup.json'
import searchZhCN from './locales/zh-CN/search.json'
import commonEn from './locales/en/common.json'
import authEn from './locales/en/auth.json'
import setupEn from './locales/en/setup.json'
import mapEn from './locales/en/map.json'
import placeEn from './locales/en/place.json'
import createEn from './locales/en/create.json'
import editEn from './locales/en/edit.json'
import shareEn from './locales/en/share.json'
import meEn from './locales/en/me.json'
import profileEn from './locales/en/profile.json'
import invitesEn from './locales/en/invites.json'
import noticesEn from './locales/en/notices.json'
import leaveEn from './locales/en/leave.json'
import personalExportEn from './locales/en/personalExport.json'
import layerExportEn from './locales/en/layerExport.json'
import adminEn from './locales/en/admin.json'
import membersEn from './locales/en/members.json'
import reportsEn from './locales/en/reports.json'
import backupEn from './locales/en/backup.json'
import searchEn from './locales/en/search.json'

export const zhCN = {
  common: commonZhCN,
  auth: authZhCN,
  setup: setupZhCN,
  map: mapZhCN,
  place: placeZhCN,
  create: createZhCN,
  edit: editZhCN,
  share: shareZhCN,
  me: meZhCN,
  profile: profileZhCN,
  invites: invitesZhCN,
  notices: noticesZhCN,
  leave: leaveZhCN,
  personalExport: personalExportZhCN,
  layerExport: layerExportZhCN,
  admin: adminZhCN,
  members: membersZhCN,
  reports: reportsZhCN,
  backup: backupZhCN,
  search: searchZhCN,
}

/** The namespaces and their keys, from the source locale. */
export type Namespaces = typeof zhCN
export type Namespace = keyof Namespaces

export const en: Namespaces = {
  common: commonEn,
  auth: authEn,
  setup: setupEn,
  map: mapEn,
  place: placeEn,
  create: createEn,
  edit: editEn,
  share: shareEn,
  me: meEn,
  profile: profileEn,
  invites: invitesEn,
  notices: noticesEn,
  leave: leaveEn,
  personalExport: personalExportEn,
  layerExport: layerExportEn,
  admin: adminEn,
  members: membersEn,
  reports: reportsEn,
  backup: backupEn,
  search: searchEn,
}

export const namespaces = Object.keys(zhCN) as Namespace[]
