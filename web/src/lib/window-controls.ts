/** The installed PWA uses `window-controls-overlay`: the platform draws its window controls
 *  inside the window and publishes the usable region as `env(titlebar-area-*)`.
 *
 *  The layout reacts in CSS, but only while the overlay is really visible. A browser tab exposes
 *  the env() values too; reserving a region nobody draws there would be worse than no handling.
 */
export function trackWindowControls(): void {
    const overlay = (
        navigator as Navigator & {
            windowControlsOverlay?: {
                visible: boolean;
                addEventListener(type: 'geometrychange', listener: () => void): void;
            };
        }
    ).windowControlsOverlay;
    if (!overlay) return;

    const apply = (): void => {
        document.documentElement.dataset.wco = overlay.visible ? '1' : '0';
    };
    apply();
    overlay.addEventListener('geometrychange', apply);
}
