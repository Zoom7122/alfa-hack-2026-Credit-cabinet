const money = new Intl.NumberFormat('ru-RU', { style: 'currency', currency: 'RUB', maximumFractionDigits: 0 });

/** 5000000 → «5 000 000 ₽». Деньги в API — целые рубли. */
export const formatMoney = (rub: number) => money.format(rub);

export function formatMonths(n: number) {
    return `${n} мес.`;
}

/** 24 → «24 месяца» */
export function formatMonthsLong(n: number) {
    return `${n} ${pluralize(n, 'месяц', 'месяца', 'месяцев')}`;
}

const compact = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 1 });

/** 300000 → «300 тыс.», 30000000 → «30 млн», 1500000 → «1,5 млн». */
export function formatCompactMoney(rub: number) {
    if (Math.abs(rub) >= 1_000_000) return `${compact.format(rub / 1_000_000)} млн`;
    if (Math.abs(rub) >= 1_000) return `${compact.format(rub / 1_000)} тыс.`;
    return compact.format(rub);
}

const rate = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 2 });

/** 18.5 → «18,5%» */
export const formatRate = (value: number) => `${rate.format(value)}%`;

const sizeFmt = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 1 });

export function formatFileSize(bytes: number) {
    if (bytes >= 1024 * 1024) return `${sizeFmt.format(bytes / 1024 / 1024)} МБ`;
    return `${sizeFmt.format(Math.max(1, Math.round(bytes / 1024)))} КБ`;
}

/** «2019-03-14» → «14.03.2019» без парсинга (часовые пояса не сдвигают дату). */
export function formatIsoDate(date: string) {
    const [y, m, d] = date.slice(0, 10).split('-');
    return y && m && d ? `${d}.${m}.${y}` : date;
}

const dayFmt = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long' });
const timeFmt = new Intl.DateTimeFormat('ru-RU', { hour: '2-digit', minute: '2-digit' });

/** «сегодня в 14:20», «вчера», «12 мая». */
export function formatUpdated(iso: string, now = new Date()) {
    const d = new Date(iso);
    const startOfDay = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
    const days = Math.round((startOfDay(now) - startOfDay(d)) / 86_400_000);
    if (days === 0) return `сегодня в ${timeFmt.format(d)}`;
    if (days === 1) return 'вчера';
    return dayFmt.format(d);
}

export function formatDay(iso: string) {
    return dayFmt.format(new Date(iso));
}

export const formatTime = (iso: string) => timeFmt.format(new Date(iso));

const dateFmt = new Intl.DateTimeFormat('ru-RU', { day: '2-digit', month: '2-digit', year: 'numeric' });

/** Момент времени → «12.05.2026» в часовом поясе пользователя. */
export const formatDate = (iso: string) => dateFmt.format(new Date(iso));

/** «Петров Пётр Петрович» → { last: 'Петров', first: 'Пётр', middle: 'Петрович' } */
export function splitName(full: string) {
    const [last = '', first = '', middle = ''] = full.trim().split(/\s+/);
    return { last, first, middle };
}

/** «Петров Пётр Петрович» → «Петров П. П.» */
export function shortName(full: string) {
    const { last, first, middle } = splitName(full);
    return [last, first && `${first[0]}.`, middle && `${middle[0]}.`].filter(Boolean).join(' ');
}

/** «Петров Пётр Петрович» → «ПП» */
export function initials(full: string) {
    const { last, first } = splitName(full);
    return `${last.charAt(0)}${first.charAt(0)}`.toUpperCase();
}

export function pluralize(n: number, one: string, few: string, many: string) {
    const mod10 = n % 10;
    const mod100 = n % 100;
    if (mod10 === 1 && mod100 !== 11) return one;
    if (mod10 >= 2 && mod10 <= 4 && (mod100 < 10 || mod100 >= 20)) return few;
    return many;
}
