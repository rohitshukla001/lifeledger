import { Pipe, PipeTransform } from '@angular/core';
import { ago, day, dueLabel, money } from './format';

@Pipe({ name: 'money' })
export class MoneyPipe implements PipeTransform {
  transform(amount: string | undefined, currency?: string): string {
    return money(amount, currency);
  }
}

@Pipe({ name: 'day' })
export class DayPipe implements PipeTransform {
  transform(date: string | undefined): string {
    return day(date);
  }
}

@Pipe({ name: 'due' })
export class DuePipe implements PipeTransform {
  transform(days: number | undefined): string {
    return dueLabel(days);
  }
}

@Pipe({ name: 'ago' })
export class AgoPipe implements PipeTransform {
  transform(timestamp: string | null | undefined): string {
    return timestamp ? ago(timestamp) : '';
  }
}
