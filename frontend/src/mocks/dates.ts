const DAY = 86_400_000;

/** Момент через n дней — ISO-строкой. */
export const addDaysIso = (from: number, days: number) => new Date(from + days * DAY).toISOString();
