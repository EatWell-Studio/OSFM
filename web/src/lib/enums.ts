// The fixed values of places, recommendations, reports and notices, in the order the app lists them. Their labels are
// in the "common" namespace under the same names (common.attitude.must_try, …); locales test checks that every value
// has one. The server accepts the same values (places and recommendations: migrations and internal/store).

/** A member's verdict on a place (C2): one per recommendation, required. */
export const attitudes = ['must_try', 'good', 'ok', 'avoid'] as const
export type Attitude = (typeof attitudes)[number]

/** When a place is worth going (A3). */
export const timeSlots = ['breakfast', 'lunch', 'afternoon', 'dinner', 'late_night'] as const
export type TimeSlot = (typeof timeSlots)[number]

/** How exactly a place's point is known (B3). */
export const precisions = ['exact', 'approx', 'area'] as const
export type Precision = (typeof precisions)[number]

/** The food categories (B5, H1): OpenStreetMap tag values, the same whitelist as store.PlaceCategories. */
export const categories = [
  'restaurant',
  'fast_food',
  'cafe',
  'food_court',
  'ice_cream',
  'bakery',
  'pastry',
  'street_vendor',
  'bar',
  'pub',
  'biergarten',
] as const
export type Category = (typeof categories)[number]

/** The cuisines a place can have (several): common values of OpenStreetMap's cuisine=* tag. */
export const cuisines = [
  'chinese',
  'sichuan',
  'cantonese',
  'hunan',
  'dongbei',
  'uyghur',
  'taiwanese',
  'hot_pot',
  'dim_sum',
  'dumpling',
  'noodle',
  'barbecue',
  'japanese',
  'sushi',
  'ramen',
  'korean',
  'vietnamese',
  'thai',
  'indian',
  'asian',
  'german',
  'italian',
  'pizza',
  'french',
  'spanish',
  'greek',
  'turkish',
  'kebab',
  'balkan',
  'middle_eastern',
  'mexican',
  'american',
  'burger',
  'sandwich',
  'seafood',
  'steak_house',
  'coffee_shop',
  'bubble_tea',
] as const
export type Cuisine = (typeof cuisines)[number]

/** Where a recommendation's knowledge comes from (a source's kind). */
export const sourceKinds = ['visited', 'chat', 'note', 'other'] as const
export type SourceKind = (typeof sourceKinds)[number]

/** Who can see a recommendation. "federated" is reserved and not offered. */
export const visibilities = ['private', 'circle'] as const
export type Visibility = (typeof visibilities)[number]

/** Which of the member's names a recommendation is signed with; fixed once created. */
export const signatures = ['main', 'pseudonym'] as const
export type Signature = (typeof signatures)[number]

/** Why a recommendation is reported (D11). */
export const reportReasons = [
  'advertising',
  'abuse',
  'political_illegal',
  'unverified_claim',
  'false_or_duplicate',
  'other',
] as const
export type ReportReason = (typeof reportReasons)[number]

/** What a notice to a member is about. */
export const noticeKinds = [
  'maintainer_edited',
  'hidden_by_maintainer',
  'hidden_by_reports',
  'restored',
  'violation_deleted',
] as const
export type NoticeKind = (typeof noticeKinds)[number]

/** Each list with the common namespace's key its labels are under. */
export const enumLabels = {
  attitude: attitudes,
  timeSlot: timeSlots,
  precision: precisions,
  category: categories,
  cuisine: cuisines,
  sourceKind: sourceKinds,
  visibility: visibilities,
  signature: signatures,
  reportReason: reportReasons,
  noticeKind: noticeKinds,
} as const
