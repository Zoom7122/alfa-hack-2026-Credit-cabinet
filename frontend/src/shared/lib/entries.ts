/** Запись повторяемого списка в форме: поля ещё могут быть не заполнены, key — для React. */
export type Entry<T> = Partial<T> & { key: string };

let seq = 0;
export const entryKey = () => `entry-${++seq}`;

export const toEntries = <T extends object>(items: T[] | undefined): Entry<T>[] =>
    (items ?? []).map((item) => ({ ...item, key: entryKey() }));

/** Только полностью заполненные записи — неполные контракт не примет. */
export function completeEntries<T>(entries: Entry<T>[], isComplete: (e: Entry<T>) => boolean): T[] {
    return entries.filter(isComplete).map(({ key: _key, ...rest }) => rest as T);
}
