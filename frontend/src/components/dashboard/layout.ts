/**
 * Shared sizing for dashboard feed cards (recent cracks, activity), so cards
 * in the same grid row line up and long lists scroll inside the card.
 */
export const FEED_BODY_HEIGHT = 320;

/** List styles for a feed body: fixed height from md up, capped on phones. */
export const feedListSx = {
  flex: 1,
  minHeight: 0,
  height: { xs: 'auto', md: FEED_BODY_HEIGHT },
  maxHeight: FEED_BODY_HEIGHT,
  overflowY: 'auto',
} as const;
