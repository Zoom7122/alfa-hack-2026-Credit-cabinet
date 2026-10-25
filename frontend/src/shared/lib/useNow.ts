import { useEffect, useState } from 'react';

/** Текущее время, обновляемое раз в stepMs, — для счётчиков вроде SLA. */
export function useNow(stepMs = 60_000) {
    const [now, setNow] = useState(() => Date.now());
    useEffect(() => {
        const timer = setInterval(() => setNow(Date.now()), stepMs);
        return () => clearInterval(timer);
    }, [stepMs]);
    return now;
}
