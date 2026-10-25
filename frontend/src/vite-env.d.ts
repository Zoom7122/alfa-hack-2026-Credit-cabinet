/// <reference types="vite/client" />

interface ImportMetaEnv {
    /** 'false' — отключить моки MSW и ходить в настоящий API */
    readonly VITE_API_MOCK?: string;
    /** База REST API, по умолчанию /api/v1 */
    readonly VITE_API_BASE_URL?: string;
    /** 'false' — ходить в живой data-collector (через прокси /collector), иначе его ответы отдаёт MSW */
    readonly VITE_COLLECTOR_MOCK?: string;
    /** База API data-collector, по умолчанию /collector/api/v1 */
    readonly VITE_COLLECTOR_BASE_URL?: string;
}

interface ImportMeta {
    readonly env: ImportMetaEnv;
}
