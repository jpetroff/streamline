import { describe, expect, test } from 'bun:test';
import { COLUMNS_PANEL, ROW_DETAILS_PANEL, clampPanelWidth, draggedPanelWidth, keyboardPanelWidth, panelWidthLimits } from '../src/lib/side-panels';

describe('shared side panel policy', () => {
  test('keeps existing initial widths within the shared viewport cap', () => {
    expect(COLUMNS_PANEL.defaultWidth(1280, 16)).toBe(288);
    expect(ROW_DETAILS_PANEL.defaultWidth(1280, 16)).toBe(409.6);
    expect(ROW_DETAILS_PANEL.defaultWidth(2400, 16)).toBe(480);
    expect(clampPanelWidth(ROW_DETAILS_PANEL.defaultWidth(800, 16), 800)).toBe(264);
  });

  test('includes both panels within 66 percent even on narrow viewports', () => {
    for (const viewport of [0, 320, 600, 1000, 1920]) {
      const width = clampPanelWidth(10000, viewport);
      expect(width).toBe(viewport * 0.33);
      expect(width * 2).toBeLessThanOrEqual(viewport * 0.66);
      expect(clampPanelWidth(0, viewport)).toBe(Math.min(240, viewport * 0.33));
    }
    expect(panelWidthLimits(600)).toEqual({ minimum: 198, maximum: 198 });
  });

  test('temporary viewport clamping retains the preferred pixel width', () => {
    const preferred = 380;
    expect(clampPanelWidth(preferred, 1280)).toBe(380);
    expect(clampPanelWidth(preferred, 800)).toBe(264);
    expect(clampPanelWidth(preferred, 1280)).toBe(380);
  });

  test('left and right handles move in opposite resize directions and stop at limits', () => {
    expect(draggedPanelWidth('left', 300, 32, 1280)).toBe(332);
    expect(draggedPanelWidth('right', 300, 32, 1280)).toBe(268);
    expect(draggedPanelWidth('left', 300, -200, 1280)).toBe(240);
    expect(draggedPanelWidth('right', 300, -1000, 1280)).toBeCloseTo(422.4);
  });

  test('keyboard arrows follow the divider and Home/End choose bounds', () => {
    for (const side of ['left', 'right'] as const) {
      expect(keyboardPanelWidth(side, 300, 'ArrowRight', 1280)).toBe(side === 'left' ? 316 : 284);
      expect(keyboardPanelWidth(side, 300, 'ArrowLeft', 1280)).toBe(side === 'left' ? 284 : 316);
      expect(keyboardPanelWidth(side, 300, 'Home', 1280)).toBe(240);
      expect(keyboardPanelWidth(side, 300, 'End', 1280)).toBeCloseTo(422.4);
      expect(keyboardPanelWidth(side, 300, 'Escape', 1280)).toBeUndefined();
      expect(keyboardPanelWidth(side, 300, 'Home', 600)).toBe(198);
    }
  });
});
