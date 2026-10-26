import { type CollectorSystem, SYSTEM_LABELS } from '../../api/collector/types';
import type { ApplicationStatus, ScoreResult, SourceCode, SourceResult } from '../../api/types';
import { pluralize } from './format';

/* Справочники рабочего места банка. Здесь видны источники и баллы — клиенту их не показываем. */

/** Подпись источника: код контракта или система data-collector (CORE_BANKING, FNS, …). */
export const sourceLabel = (code: string | undefined) =>
    code ? (SYSTEM_LABELS[code as CollectorSystem] ?? SOURCE_LABELS[code as SourceCode] ?? code) : '—';

export const SOURCE_LABELS: Record<SourceCode, string> = {
    EGRUL: 'ЕГРЮЛ',
    EGRIP: 'ЕГРИП',
    PB_NALOG: 'ПБ ФНС',
    GIRBO: 'ГИР БО',
    BKI: 'БКИ',
    FSSP: 'ФССП',
    FEDRESURS: 'Федресурс',
    KAD: 'КАД',
    RNP: 'РНП',
    EGRN: 'ЕГРН',
    BANK_INTERNAL: 'Данные банка',
};

export const SOURCE_STATE_LABELS: Record<SourceResult['state'], string> = {
    PENDING: 'в очереди',
    RUNNING: 'собираем…',
    DONE: 'получено',
    FAILED: 'ошибка',
    SKIPPED: 'не нужен',
};

export const SOURCE_MODE_LABELS: Record<NonNullable<SourceResult['mode']>, string> = {
    LIVE: 'живой запрос',
    MOCK: 'mock',
    CACHE: 'кэш',
};

type Color = 'green' | 'orange' | 'red' | 'grey' | 'blue';

export const BO_STATUS_BADGES: Partial<Record<ApplicationStatus, { label: string; color: Color }>> = {
    SUBMITTED: { label: 'В очереди', color: 'grey' },
    ENRICHING: { label: 'Сбор данных', color: 'blue' },
    VERIFYING: { label: 'Проверки', color: 'blue' },
    SCORING: { label: 'Скоринг', color: 'blue' },
    DECIDING: { label: 'Решение движка', color: 'blue' },
    MANUAL_REVIEW: { label: 'Ручное рассмотрение', color: 'blue' },
    WAITING_FOR_INFO: { label: 'Ждём клиента', color: 'orange' },
    OFFERED: { label: 'Предложения у клиента', color: 'green' },
    SIGNING: { label: 'Подписание', color: 'green' },
    COMPLETED: { label: 'Выдан', color: 'green' },
    REJECTED: { label: 'Отказ', color: 'red' },
    DECLINED: { label: 'Клиент отказался', color: 'grey' },
    CANCELLED: { label: 'Отозвана', color: 'grey' },
    EXPIRED: { label: 'Истёк срок', color: 'grey' },
};

export const GRADE_COLORS: Record<ScoreResult['grade'], Color> = { A: 'green', B: 'green', C: 'orange', D: 'red', E: 'red' };

export const IN_PROCESSING: ApplicationStatus[] = ['SUBMITTED', 'ENRICHING', 'VERIFYING', 'SCORING', 'DECIDING'];

const HOUR = 3_600_000;

/** Сколько осталось до SLA: «1 ч 20 мин», «1 д 4 ч»; срочно — меньше 4 часов. */
export function slaLeft(due: string | null | undefined, now = Date.now()) {
    if (!due) return undefined;
    const ms = new Date(due).getTime() - now;
    if (ms <= 0) return { text: 'просрочен', urgent: true, overdue: true };
    const minutes = Math.floor(ms / 60_000);
    const days = Math.floor(minutes / (24 * 60));
    const hours = Math.floor((minutes % (24 * 60)) / 60);
    const mins = minutes % 60;
    const text = days ? `${days} д${hours ? ` ${hours} ч` : ''}` : hours ? `${hours} ч${mins ? ` ${mins} мин` : ''}` : `${mins} мин`;
    return { text, urgent: ms < 4 * HOUR, overdue: false };
}

/** До конца сегодняшнего дня (по часам пользователя). */
export function dueToday(due: string | null | undefined, now = new Date()) {
    if (!due) return false;
    const end = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1).getTime();
    return new Date(due).getTime() < end;
}

export const LIMITED_BY_LABELS: Record<NonNullable<NonNullable<ScoreResult['max_amount']>['limited_by']>, string> = {
    PRODUCT_LIMIT: 'лимитом продукта',
    REVENUE: 'выручкой',
    DEBT_SERVICE: 'долговой нагрузкой',
    COLLATERAL: 'залогом',
};

export const sourcesCountText = (n: number) => `${n} ${pluralize(n, 'источник', 'источника', 'источников')}`;
