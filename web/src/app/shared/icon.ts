import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';

const PATHS: Record<string, string> = {
  today:
    'M12 3v2M12 19v2M4.2 4.2l1.4 1.4M18.4 18.4l1.4 1.4M3 12h2M19 12h2M4.2 19.8l1.4-1.4M18.4 5.6l1.4-1.4M8 12a4 4 0 1 0 8 0a4 4 0 1 0-8 0',
  chat: 'M4 5h16v11H9l-5 4z',
  ledger: 'M5 3h12a2 2 0 0 1 2 2v16H7a2 2 0 0 1-2-2zM9 3v18M12 8h4M12 12h4',
  approvals: 'M4 12l5 5L20 6',
  import: 'M12 4v11M7 10l5 5l5-5M5 20h14',
  memory: 'M12 3l8 9l-8 9l-8-9z',
  activity: 'M4 6h16M4 12h10M4 18h13',
};

@Component({
  selector: 'll-icon',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" focusable="false">
    <path [attr.d]="path()" />
  </svg>`,
  styles: `
    :host {
      display: inline-flex;
    }
    svg {
      fill: none;
      stroke: currentColor;
      stroke-width: 1.7;
      stroke-linecap: round;
      stroke-linejoin: round;
    }
  `,
})
export class Icon {
  readonly name = input.required<string>();
  protected readonly path = computed(() => PATHS[this.name()] ?? '');
}
