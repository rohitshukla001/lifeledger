import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';
import { DayPipe, MoneyPipe } from '../core/pipes';
import { Proposal } from '../core/models';

@Component({
  selector: 'll-proposal-card',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DayPipe, MoneyPipe],
  template: `
    <article class="card proposal" [class.dup]="proposal().duplicate_of !== null">
      <header>
        <div>
          <span class="tag">{{ proposal().category }}</span>
          <h3>{{ proposal().title }}</h3>
        </div>
        <div class="confidence" [title]="'Model confidence ' + percent() + '%'">
          <span class="bar"><span [style.width.%]="percent()"></span></span>
          <span class="mono">{{ percent() }}%</span>
        </div>
      </header>
      <dl>
        @if (proposal().amount) {
          <div>
            <dt>Amount</dt>
            <dd class="mono">{{ proposal().amount | money: proposal().currency }}</dd>
          </div>
        }
        <div>
          <dt>Due</dt>
          <dd class="mono">{{ (proposal().due_on | day) || 'No date' }}</dd>
        </div>
        @if (proposal().recurrence !== 'none') {
          <div>
            <dt>Repeats</dt>
            <dd>{{ proposal().recurrence }}</dd>
          </div>
        }
      </dl>
      @if (proposal().evidence) {
        <blockquote>“{{ proposal().evidence }}”</blockquote>
      }
      @if (proposal().duplicate_of !== null) {
        <p class="warn">
          Looks like item #{{ proposal().duplicate_of }} that is already in your ledger.
        </p>
      }
      <footer>
        <button class="btn primary small" type="button" [disabled]="busy()" (click)="accept.emit()">
          Add to ledger
        </button>
        <button class="btn ghost small" type="button" [disabled]="busy()" (click)="reject.emit()">
          Discard
        </button>
      </footer>
    </article>
  `,
  styles: `
    .proposal {
      padding: 1rem 1.1rem;
      display: grid;
      gap: 0.75rem;
      border-left: 4px solid var(--green-2);
    }
    .proposal.dup {
      border-left-color: var(--ochre);
    }
    header {
      display: flex;
      justify-content: space-between;
      gap: 1rem;
      align-items: start;
    }
    h3 {
      margin-top: 0.35rem;
    }
    .confidence {
      display: flex;
      align-items: center;
      gap: 0.4rem;
      font-size: 0.75rem;
      color: var(--ink-3);
    }
    .bar {
      width: 56px;
      height: 6px;
      border-radius: 3px;
      background: var(--paper-2);
      overflow: hidden;
    }
    .bar span {
      display: block;
      height: 100%;
      background: var(--green-2);
    }
    dl {
      display: flex;
      flex-wrap: wrap;
      gap: 0.5rem 1.75rem;
      margin: 0;
    }
    dt {
      font-size: 0.7rem;
      text-transform: uppercase;
      letter-spacing: 0.1em;
      color: var(--ink-3);
    }
    dd {
      margin: 0;
    }
    blockquote {
      margin: 0;
      padding-left: 0.75rem;
      border-left: 2px solid var(--rule-strong);
      font-family: var(--font-display);
      font-style: italic;
      color: var(--ink-2);
    }
    .warn {
      margin: 0;
      font-size: 0.85rem;
      color: var(--ochre);
    }
    footer {
      display: flex;
      gap: 0.5rem;
    }
  `,
})
export class ProposalCard {
  readonly proposal = input.required<Proposal>();
  readonly busy = input(false);
  readonly accept = output();
  readonly reject = output();

  protected readonly percent = computed(() => Math.round(this.proposal().confidence * 100));
}
