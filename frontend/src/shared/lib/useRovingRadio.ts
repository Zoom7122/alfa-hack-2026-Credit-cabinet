import { type KeyboardEvent, useRef } from 'react';

/** Стрелки перемещают выбор и фокус внутри role=radiogroup (паттерн WAI-ARIA). */
export function useRovingRadio<T>(items: T[], onSelect: (item: T) => void) {
    const ref = useRef<HTMLDivElement>(null);
    const onKeyDown = (index: number) => (e: KeyboardEvent<HTMLElement>) => {
        const delta = e.key === 'ArrowDown' || e.key === 'ArrowRight' ? 1 : e.key === 'ArrowUp' || e.key === 'ArrowLeft' ? -1 : 0;
        if (!delta || !items.length) return;
        e.preventDefault();
        const next = (index + delta + items.length) % items.length;
        const item = items[next];
        if (item === undefined) return;
        onSelect(item);
        ref.current?.querySelectorAll<HTMLElement>('[role="radio"]')[next]?.focus();
    };
    return { ref, onKeyDown };
}
