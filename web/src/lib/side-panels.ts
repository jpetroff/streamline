/** Shared policy for every side panel; widths include the resize divider. */
export const SIDE_PANEL_LIMITS = { minimum: 240, viewportFraction: 0.33, keyboardStep: 16 } as const;

/** Content-independent metadata supplied to the shared panel shell. */
export interface SidePanelDefinition {
  id: string;
  label: string;
  side: 'left' | 'right';
  initiallyOpen: boolean;
  defaultWidth: (viewportWidth: number, rem: number) => number;
}

export const COLUMNS_PANEL: SidePanelDefinition = {
  id: 'columns-panel', label: 'Columns and filters', side: 'left', initiallyOpen: true,
  defaultWidth: (_viewportWidth, rem) => 18 * rem,
};

export const ROW_DETAILS_PANEL: SidePanelDefinition = {
  id: 'row-details-panel', label: 'Row details', side: 'right', initiallyOpen: false,
  defaultWidth: (viewportWidth, rem) => Math.min(30 * rem, Math.max(20 * rem, viewportWidth * 0.32)),
};

/** The viewport cap wins when the usual minimum cannot fit. */
export function panelWidthLimits(viewportWidth: number) {
  const maximum = Math.max(0, viewportWidth) * SIDE_PANEL_LIMITS.viewportFraction;
  return { minimum: Math.min(SIDE_PANEL_LIMITS.minimum, maximum), maximum };
}

/** Resolve a temporary display width without overwriting the user's preference. */
export function clampPanelWidth(width: number, viewportWidth: number): number {
  const { minimum, maximum } = panelWidthLimits(viewportWidth);
  return Math.min(maximum, Math.max(minimum, width));
}

/** Convert a physical divider movement to the width of its adjacent panel. */
export function draggedPanelWidth(side: SidePanelDefinition['side'], startWidth: number, deltaX: number, viewportWidth: number): number {
  return clampPanelWidth(startWidth + (side === 'left' ? deltaX : -deltaX), viewportWidth);
}

/** Keyboard equivalent of divider movement; unrelated keys remain unhandled. */
export function keyboardPanelWidth(side: SidePanelDefinition['side'], width: number, key: string, viewportWidth: number): number | undefined {
  const { minimum, maximum } = panelWidthLimits(viewportWidth);
  if (key === 'Home') return minimum;
  if (key === 'End') return maximum;
  if (key !== 'ArrowLeft' && key !== 'ArrowRight') return undefined;
  return draggedPanelWidth(side, width, key === 'ArrowRight' ? SIDE_PANEL_LIMITS.keyboardStep : -SIDE_PANEL_LIMITS.keyboardStep, viewportWidth);
}
