import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { DueTone } from '../core/format';

const labels: Record<string, [string, string]> = {
  overdue: ['Overdue', 'red'],
  today: ['Due today', 'red'],
  soon: ['Due soon', 'ochre'],
  done: ['Done', 'green'],
  snoozed: ['Snoozed', 'grey'],
  cancelled: ['Void', 'grey'],
  pending: ['Waiting', 'ochre'],
  approved: ['Approved', 'green'],
  rejected: ['Rejected', 'grey'],
  failed: ['Failed', 'red'],
  accepted: ['Accepted', 'green'],
};

@Component({
  selector: 'll-stamp',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `@if (spec(); as s) {
    <span [class]="'stamp ' + s[1]">{{ s[0] }}</span>
  }`,
})
export class Stamp {
  readonly state = input.required<string | DueTone>();
  protected readonly spec = computed(() => labels[this.state()] ?? null);
}
