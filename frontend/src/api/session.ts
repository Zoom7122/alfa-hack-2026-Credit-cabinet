const TOKEN_KEY = 'credit-cabinet:token';

type Listener = () => void;
const listeners = new Set<Listener>();

function read(): string | null {
    try {
        return localStorage.getItem(TOKEN_KEY);
    } catch {
        return null;
    }
}

let token = read();

function set(next: string | null) {
    token = next;
    listeners.forEach((l) => l());
}

// выход или вход в соседней вкладке
if (typeof window !== 'undefined') {
    window.addEventListener('storage', (e) => {
        if (e.key === TOKEN_KEY) set(e.newValue);
    });
}

/** Токен мок-авторизации (POST /auth/mock-login). Хранится в localStorage, чтобы пережить перезагрузку. */
export const session = {
    getToken: () => token,
    setToken(next: string) {
        try {
            localStorage.setItem(TOKEN_KEY, next);
        } catch {
            /* приватный режим — живём без сохранения */
        }
        set(next);
    },
    clear() {
        if (token === null) return;
        try {
            localStorage.removeItem(TOKEN_KEY);
        } catch {
            /* ignore */
        }
        set(null);
    },
    subscribe(listener: Listener) {
        listeners.add(listener);
        return () => {
            listeners.delete(listener);
        };
    },
};
