/** Демо-экраны основного API доступны только вместе с их моками. */
export const API_MOCK_ENABLED = import.meta.env.VITE_API_MOCK !== 'false';
/** data-collector работает и с моками, и с живым сервисом. */
export const COLLECTOR_MOCK_ENABLED = import.meta.env.VITE_COLLECTOR_MOCK !== 'false';
