const isPlainObject = (v: unknown): v is Record<string, unknown> => Boolean(v) && typeof v === 'object' && !Array.isArray(v);

/** JSON Merge Patch (RFC 7386): объекты сливаются, массивы заменяются целиком, null удаляет поле. */
export function mergePatch<T>(target: T, patch: unknown): T {
    if (!isPlainObject(patch)) return patch as T;
    const result: Record<string, unknown> = isPlainObject(target) ? { ...target } : {};
    for (const [key, value] of Object.entries(patch)) {
        if (value === null) delete result[key];
        else result[key] = mergePatch(result[key], value);
    }
    return result as T;
}

/** Склеить два патча так, чтобы применение результата равнялось применению обоих по очереди. */
export function combinePatches<T extends object>(first: T, second: T): T {
    const result: Record<string, unknown> = { ...(first as Record<string, unknown>) };
    for (const [key, value] of Object.entries(second)) {
        const prev = result[key];
        result[key] = isPlainObject(prev) && isPlainObject(value) ? combinePatches(prev, value) : value;
    }
    return result as T;
}
