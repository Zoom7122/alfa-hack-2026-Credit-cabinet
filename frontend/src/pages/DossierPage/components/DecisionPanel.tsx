import { useState } from 'react';
import { AmountInput } from '@alfalab/core-components/amount-input';
import { Button } from '@alfalab/core-components/button';
import { Checkbox } from '@alfalab/core-components/checkbox';
import { IconButton } from '@alfalab/core-components/icon-button';
import { Input } from '@alfalab/core-components/input';
import { Plate } from '@alfalab/core-components/plate';
import { Segment, SegmentedControl } from '@alfalab/core-components/segmented-control';
import { Select } from '@alfalab/core-components/select';
import { SidePanelResponsive } from '@alfalab/core-components/side-panel';
import { Textarea } from '@alfalab/core-components/textarea';
import { Typography } from '@alfalab/core-components/typography';
import { CrossMIcon } from '@alfalab/icons-glyph/CrossMIcon';
import { PlusMIcon } from '@alfalab/icons-glyph/PlusMIcon';

import { ApiError } from '../../../api/client';
import type { useBoDecide } from '../../../api/queries';
import type { BoDecisionInput, BoDossier, CollateralType, DocumentType, InfoRequestInput } from '../../../api/types';
import { COLLATERAL_LABELS, DOCUMENT_LABELS, FIELD_LABELS } from '../../../shared/lib/dictionaries';
import { entryKey } from '../../../shared/lib/entries';
import { useIdempotencyKeys } from '../../../shared/lib/idempotency';
import './DecisionPanel.css';

export type DecisionMode = 'APPROVE' | 'REQUEST_INFO' | 'REJECT';

type OfferDraft = {
    key: string;
    kind: 'REQUESTED' | 'ALTERNATIVE';
    productCode: string;
    amount: number | null;
    term: string;
    rate: string;
    validDays: string;
    guarantee: boolean;
    // условия движка андеррайтер не правит, но обязан передать клиенту
    collateral: CollateralType[];
    conditions: string[];
};
type QuestionDraft = { key: string; title: string; description: string; type: InfoRequestInput['input']['type']; field: string; required: boolean; doc: string };

const KIND_LABELS = { REQUESTED: 'Как запрошено', ALTERNATIVE: 'Альтернатива' };
const VALID_DAYS = ['3', '7', '14'].map((d) => ({ key: d, content: `${d} дней` }));
const GUARANTEE = [
    { key: 'yes', content: 'Нужно' },
    { key: 'no', content: 'Не нужно' },
];

/** Экспертные поправки к скорингу — коды для expert_adjustments. */
const ADJUSTMENTS = [
    { key: 'CONFIRMED_LARGE_CONTRACT', content: 'Крупный контракт' },
    { key: 'SEASONAL_BUSINESS', content: 'Сезонность бизнеса' },
    { key: 'OWNER_GUARANTEE', content: 'Поручительство собственника' },
    { key: 'EXTRA_COLLATERAL', content: 'Залог сверх требуемого' },
];

const ANSWER_TYPES: { key: InfoRequestInput['input']['type']; content: string }[] = [
    { key: 'MONEY', content: 'Сумма' },
    { key: 'NUMBER', content: 'Число' },
    { key: 'TEXT', content: 'Текст' },
    { key: 'DATE', content: 'Дата' },
    { key: 'BOOLEAN', content: 'Да / нет' },
    { key: 'NONE', content: 'Только документ' },
];
/** Поля анкеты, которые можно обновить ответом, — все числовые. */
const NUMERIC_ANSWERS = new Set<QuestionDraft['type']>(['MONEY', 'NUMBER']);

const FIELDS = [{ key: '', content: 'Не связано с анкетой' }, ...Object.entries(FIELD_LABELS).filter(([k]) => k.startsWith('financials.')).map(([key, content]) => ({ key, content }))];
const DOCS = [{ key: '', content: 'Документ не нужен' }, ...(Object.entries(DOCUMENT_LABELS) as [DocumentType, string][]).filter(([k]) => k !== 'SIGNATURE' && k !== 'LOAN_AGREEMENT_DRAFT').map(([key, content]) => ({ key, content }))];

/** Причины отказа — их увидит клиент, поэтому без баллов и названий источников. */
const REASONS = [
    { code: 'HIGH_DEBT_LOAD', title: 'Высокая текущая долговая нагрузка' },
    { code: 'REVENUE_NOT_CONFIRMED', title: 'Выручка не подтверждается отчётностью' },
    { code: 'AMOUNT_ABOVE_LIMIT', title: 'Запрошенная сумма больше, чем позволяет выручка' },
    { code: 'NEGATIVE_CREDIT_HISTORY', title: 'Негативная кредитная история' },
    { code: 'BUSINESS_TOO_YOUNG', title: 'Бизнес работает меньше года' },
];

const parseNumber = (v: string) => (v.trim() ? Number(v.replace(',', '.').replace(/\s/g, '')) : Number.NaN);
const POINTS = /^[+-]?\d{1,3}$/;

function initialOffers(dossier: BoDossier): OfferDraft[] {
    const app = dossier.application;
    const proposed = dossier.engine_decision?.proposed_offers ?? [];
    if (!proposed.length) {
        return [
            {
                key: entryKey(),
                kind: 'REQUESTED',
                productCode: app.product_code,
                amount: app.requested?.amount ?? null,
                term: String(app.requested?.term_months ?? 12),
                rate: '',
                validDays: '7',
                guarantee: false,
                collateral: [],
                conditions: [],
            },
        ];
    }
    return proposed.map((o) => ({
        key: entryKey(),
        // RETENTION в OfferInput нет — удерживающее предложение уходит как альтернатива
        kind: o.kind === 'REQUESTED' ? 'REQUESTED' : 'ALTERNATIVE',
        productCode: o.product_code,
        amount: o.amount,
        term: String(o.term_months),
        rate: String(o.rate_annual).replace('.', ','),
        validDays: '7',
        guarantee: Boolean(o.guarantee_required),
        collateral: o.collateral_required ?? [],
        conditions: o.conditions ?? [],
    }));
}

/** Вопрос по самому заметному расхождению — чтобы не начинать с пустой формы. */
function initialQuestions(dossier: BoDossier): QuestionDraft[] {
    const revenue = dossier.findings.find((f) => f.material && f.field === 'financials.annual_revenue');
    const year = dossier.application.financials?.annual_revenue_year ?? 2025;
    return [
        {
            key: entryKey(),
            title: revenue ? `Уточните выручку за ${year} год` : '',
            description: revenue ? 'Это поможет подобрать для вас максимальную сумму' : '',
            type: revenue ? 'MONEY' : 'TEXT',
            field: revenue ? 'financials.annual_revenue' : '',
            required: true,
            doc: revenue ? 'FINANCIAL_STATEMENTS' : '',
        },
    ];
}

function offerErrors(o: OfferDraft) {
    const term = parseNumber(o.term);
    const rate = parseNumber(o.rate);
    return {
        amount: (o.amount ?? 0) > 0 ? undefined : 'Укажите сумму',
        term: Number.isInteger(term) && term > 0 ? undefined : 'Укажите срок',
        rate: rate > 0 && rate < 100 ? undefined : 'Укажите ставку',
    };
}

function questionErrors(q: QuestionDraft) {
    return {
        title: q.title.trim() ? undefined : 'Сформулируйте вопрос',
        type: q.field && !NUMERIC_ANSWERS.has(q.type) ? 'Поле анкеты числовое — выберите «Сумма» или «Число»' : undefined,
        doc: q.type === 'NONE' && !q.doc ? 'Для ответа документом выберите документ' : undefined,
    };
}

const hasErrors = (e: Record<string, string | undefined>) => Object.values(e).some(Boolean);

type Props = {
    dossier: BoDossier;
    mode: DecisionMode;
    decide: ReturnType<typeof useBoDecide>;
    onModeChange: (mode: DecisionMode) => void;
    onClose: () => void;
};

/** Экран 18: решение андеррайтера — одобрить с правкой предложений, запросить данные или отказать. */
export function DecisionPanel({ dossier, mode, decide, onModeChange, onClose }: Props) {
    const app = dossier.application;
    const { keyFor, forget } = useIdempotencyKeys();
    const [offers, setOffers] = useState<OfferDraft[]>(() => initialOffers(dossier));
    const [adjustment, setAdjustment] = useState<{ code?: string; points: string }>({ points: '' });
    const [questions, setQuestions] = useState<QuestionDraft[]>(() => initialQuestions(dossier));
    const [reasons, setReasons] = useState<string[]>([]);
    const [otherReason, setOtherReason] = useState('');
    const [comment, setComment] = useState('');
    // ошибки полей показываем после первой попытки отправить
    const [tried, setTried] = useState(false);

    const editOffer = (key: string, patch: Partial<OfferDraft>) => setOffers((list) => list.map((o) => (o.key === key ? { ...o, ...patch } : o)));
    const editQuestion = (key: string, patch: Partial<QuestionDraft>) => setQuestions((list) => list.map((q) => (q.key === key ? { ...q, ...patch } : q)));

    const adjustmentError = adjustment.code
        ? POINTS.test(adjustment.points.trim()) && parseNumber(adjustment.points) !== 0
            ? undefined
            : 'Укажите баллы целым числом, например +30 или -15'
        : adjustment.points.trim()
          ? 'Выберите фактор'
          : undefined;
    const reasonsValid = reasons.length > 0 || otherReason.trim().length > 0;
    const valid =
        mode === 'APPROVE'
            ? offers.length > 0 && offers.every((o) => !hasErrors(offerErrors(o))) && !adjustmentError
            : mode === 'REQUEST_INFO'
              ? questions.length > 0 && questions.every((q) => !hasErrors(questionErrors(q)))
              : reasonsValid;
    const show = (error: string | undefined) => (tried ? error : undefined);

    function body(): BoDecisionInput {
        const common = {
            internal_comment: comment.trim() || undefined,
            expert_adjustments:
                mode === 'APPROVE' && adjustment.code
                    ? [{ code: adjustment.code, points: parseNumber(adjustment.points), comment: comment.trim() || undefined }]
                    : undefined,
        };
        if (mode === 'APPROVE') {
            return {
                action: 'APPROVE',
                ...common,
                offers: offers.map((o) => ({
                    kind: o.kind,
                    product_code: o.productCode,
                    amount: o.amount ?? 0,
                    term_months: parseNumber(o.term),
                    rate_annual: parseNumber(o.rate),
                    collateral_required: o.collateral.length ? o.collateral : undefined,
                    guarantee_required: o.guarantee,
                    conditions: o.conditions.length ? o.conditions : undefined,
                    valid_days: Number(o.validDays),
                })),
            };
        }
        if (mode === 'REQUEST_INFO') {
            return {
                action: 'REQUEST_INFO',
                ...common,
                info_requests: questions.map((q) => ({
                    title: q.title.trim(),
                    description: q.description.trim() || undefined,
                    required: q.required,
                    target_field: q.field || undefined,
                    input: { type: q.type },
                    documents: q.doc ? { required: q.type === 'NONE', accepted_types: [q.doc as DocumentType] } : undefined,
                })),
            };
        }
        return {
            action: 'REJECT',
            ...common,
            client_reasons: [
                ...REASONS.filter((r) => reasons.includes(r.code)),
                ...(otherReason.trim() ? [{ code: 'OTHER', title: otherReason.trim() }] : []),
            ],
        };
    }

    const submit = () => {
        setTried(true);
        if (!valid) return;
        const input = body();
        decide.mutate(
            { ...input, version: app.version, idempotencyKey: keyFor('decision', input) },
            {
                onSuccess: onClose,
                onError: (e) => {
                    if (e instanceof ApiError && e.status < 500) forget('decision', input);
                },
            },
        );
    };

    const changeMode = (next: DecisionMode) => {
        decide.reset();
        setTried(false);
        onModeChange(next);
    };

    /** После 412 досье уже перечитано — подставляем черновики заново по свежим данным. */
    const refill = () => {
        setOffers(initialOffers(dossier));
        setQuestions(initialQuestions(dossier));
        decide.reset();
    };

    const submitLabel = mode === 'APPROVE' ? 'Одобрить и отправить клиенту' : mode === 'REQUEST_INFO' ? 'Отправить вопросы клиенту' : 'Отказать';
    const error = decide.error instanceof ApiError ? decide.error : undefined;
    const stale = error?.status === 412;
    const fieldErrors = error?.status === 422 ? (error.problem?.errors ?? []) : [];

    return (
        <SidePanelResponsive breakpoint={600} open={true} onClose={onClose} size={500} wrapperProps={{ 'aria-label': 'Решение по заявке', 'aria-modal': true }}>
            <SidePanelResponsive.Header hasCloser={true} title='Решение по заявке' />
            <SidePanelResponsive.Content>
                <div className='decision-panel'>
                    <SegmentedControl selectedId={mode} onChange={(id) => changeMode(id as DecisionMode)}>
                        <Segment id='APPROVE' title='Одобрить' />
                        <Segment id='REQUEST_INFO' title='Запросить' />
                        <Segment id='REJECT' title='Отказать' />
                    </SegmentedControl>

                    {mode === 'APPROVE' && (
                        <section className='decision-panel__section' aria-labelledby='decision-offers'>
                            <Typography.Title id='decision-offers' tag='h3' view='xsmall' font='system'>
                                Предложения клиенту
                            </Typography.Title>
                            {offers.length === 0 && (
                                <Typography.Text view='primary-small' color='negative'>
                                    Добавьте хотя бы одно предложение
                                </Typography.Text>
                            )}
                            {offers.map((o, i) => {
                                const errors = offerErrors(o);
                                const terms = [...o.collateral.map((c) => `залог: ${COLLATERAL_LABELS[c].toLowerCase()}`), ...o.conditions];
                                return (
                                    <div key={o.key} className='decision-panel__entry'>
                                        <div className='decision-panel__entry-head'>
                                            <Typography.Text view='primary-medium' weight='bold'>
                                                Предложение {i + 1} · {KIND_LABELS[o.kind]}
                                            </Typography.Text>
                                            <IconButton
                                                icon={CrossMIcon}
                                                size={24}
                                                view='secondary'
                                                onClick={() => setOffers((list) => list.filter((x) => x.key !== o.key))}
                                                aria-label={`Убрать предложение ${i + 1}`}
                                            />
                                        </div>
                                        <div className='decision-panel__grid'>
                                            <AmountInput
                                                label='Сумма'
                                                value={o.amount}
                                                minority={1}
                                                integersOnly={true}
                                                onChange={(_, { value }) => editOffer(o.key, { amount: value })}
                                                error={show(errors.amount)}
                                                block={true}
                                                size={56}
                                            />
                                            <Input
                                                label='Срок, мес.'
                                                value={o.term}
                                                inputMode='numeric'
                                                onChange={(_, { value }) => editOffer(o.key, { term: value.replace(/\D/g, '') })}
                                                error={show(errors.term)}
                                                block={true}
                                                size={56}
                                            />
                                            <Input
                                                label='Ставка, %'
                                                value={o.rate}
                                                inputMode='decimal'
                                                onChange={(_, { value }) => editOffer(o.key, { rate: value.replace(/[^\d.,]/g, '') })}
                                                error={show(errors.rate)}
                                                block={true}
                                                size={56}
                                            />
                                            <Select label='Действует' size={56} block={true} options={VALID_DAYS} selected={o.validDays} onChange={({ selected }) => selected && editOffer(o.key, { validDays: selected.key })} />
                                            <Select
                                                label='Поручительство'
                                                size={56}
                                                block={true}
                                                options={GUARANTEE}
                                                selected={o.guarantee ? 'yes' : 'no'}
                                                onChange={({ selected }) => selected && editOffer(o.key, { guarantee: selected.key === 'yes' })}
                                            />
                                        </div>
                                        {terms.length > 0 && (
                                            <Typography.Text view='primary-small' color='secondary'>
                                                Условия движка: {terms.join('; ')}
                                            </Typography.Text>
                                        )}
                                    </div>
                                );
                            })}
                            <div>
                                <Button
                                    view='text'
                                    size={32}
                                    leftAddons={<PlusMIcon />}
                                    onClick={() =>
                                        setOffers((list) => [
                                            ...list,
                                            {
                                                key: entryKey(),
                                                kind: 'ALTERNATIVE',
                                                productCode: app.product_code,
                                                amount: app.requested?.amount ?? null,
                                                term: String(app.requested?.term_months ?? 12),
                                                rate: '',
                                                validDays: '7',
                                                guarantee: false,
                                                collateral: [],
                                                conditions: [],
                                            },
                                        ])
                                    }
                                >
                                    Добавить предложение
                                </Button>
                            </div>
                            <Typography.Title tag='h3' view='xsmall' font='system'>
                                Экспертная поправка к скорингу
                            </Typography.Title>
                            <div className='decision-panel__grid'>
                                <Select
                                    label='Фактор'
                                    size={56}
                                    block={true}
                                    options={ADJUSTMENTS}
                                    selected={adjustment.code ?? null}
                                    allowUnselect={true}
                                    onChange={({ selected }) => setAdjustment((a) => ({ ...a, code: selected?.key }))}
                                />
                                <Input
                                    label='Баллы'
                                    value={adjustment.points}
                                    onChange={(_, { value }) => setAdjustment((a) => ({ ...a, points: value.replace(/[^\d+-]/g, '') }))}
                                    error={show(adjustmentError)}
                                    block={true}
                                    size={56}
                                />
                            </div>
                        </section>
                    )}

                    {mode === 'REQUEST_INFO' && (
                        <section className='decision-panel__section' aria-labelledby='decision-questions'>
                            <Typography.Title id='decision-questions' tag='h3' view='xsmall' font='system'>
                                Вопросы клиенту
                            </Typography.Title>
                            <Typography.Text view='primary-small' color='secondary'>
                                Формулируйте нейтрально: клиент не должен узнать, где мы нашли расхождение.
                            </Typography.Text>
                            {questions.length === 0 && (
                                <Typography.Text view='primary-small' color='negative'>
                                    Добавьте хотя бы один вопрос
                                </Typography.Text>
                            )}
                            {questions.map((q, i) => {
                                const errors = questionErrors(q);
                                return (
                                    <div key={q.key} className='decision-panel__entry'>
                                        <div className='decision-panel__entry-head'>
                                            <Typography.Text view='primary-medium' weight='bold'>
                                                Вопрос {i + 1}
                                            </Typography.Text>
                                            <IconButton
                                                icon={CrossMIcon}
                                                size={24}
                                                view='secondary'
                                                onClick={() => setQuestions((list) => list.filter((x) => x.key !== q.key))}
                                                aria-label={`Убрать вопрос ${i + 1}`}
                                            />
                                        </div>
                                        <Input label='Вопрос' value={q.title} onChange={(_, { value }) => editQuestion(q.key, { title: value })} error={show(errors.title)} block={true} size={56} />
                                        <Input label='Пояснение для клиента' value={q.description} onChange={(_, { value }) => editQuestion(q.key, { description: value })} block={true} size={56} />
                                        <div className='decision-panel__grid'>
                                            <Select
                                                label='Ответ'
                                                size={56}
                                                block={true}
                                                options={ANSWER_TYPES}
                                                selected={q.type}
                                                onChange={({ selected }) => selected && editQuestion(q.key, { type: selected.key as QuestionDraft['type'] })}
                                                error={show(errors.type)}
                                            />
                                            <Select label='Обновит поле' size={56} block={true} options={FIELDS} selected={q.field} onChange={({ selected }) => editQuestion(q.key, { field: selected?.key ?? '' })} />
                                        </div>
                                        <Select label='Документ' size={56} block={true} options={DOCS} selected={q.doc} onChange={({ selected }) => editQuestion(q.key, { doc: selected?.key ?? '' })} error={show(errors.doc)} />
                                        <Checkbox label='Обязательный вопрос' checked={q.required} onChange={(_, { checked }) => editQuestion(q.key, { required: checked })} size={24} />
                                    </div>
                                );
                            })}
                            <div>
                                <Button
                                    view='text'
                                    size={32}
                                    leftAddons={<PlusMIcon />}
                                    onClick={() => setQuestions((list) => [...list, { key: entryKey(), title: '', description: '', type: 'TEXT', field: '', required: true, doc: '' }])}
                                >
                                    Добавить вопрос
                                </Button>
                            </div>
                        </section>
                    )}

                    {mode === 'REJECT' && (
                        <section className='decision-panel__section' aria-labelledby='decision-reasons'>
                            <Typography.Title id='decision-reasons' tag='h3' view='xsmall' font='system'>
                                Что увидит клиент
                            </Typography.Title>
                            <Plate view='common'>Клиенту покажем только эти формулировки — без баллов и названий источников.</Plate>
                            <div className='decision-panel__reasons' role='group' aria-labelledby='decision-reasons'>
                                {REASONS.map((r) => (
                                    <Checkbox
                                        key={r.code}
                                        label={r.title}
                                        checked={reasons.includes(r.code)}
                                        onChange={(_, { checked }) => setReasons((list) => (checked ? [...list, r.code] : list.filter((c) => c !== r.code)))}
                                        size={24}
                                    />
                                ))}
                            </div>
                            <Input
                                label='Другая причина'
                                value={otherReason}
                                onChange={(_, { value }) => setOtherReason(value)}
                                error={show(reasonsValid ? undefined : 'Выберите причину или опишите свою')}
                                block={true}
                                size={56}
                            />
                        </section>
                    )}

                    <Textarea
                        label='Внутренний комментарий (клиент не увидит)'
                        aria-label='Внутренний комментарий (клиент не увидит)'
                        value={comment}
                        onChange={(e) => setComment(e.target.value)}
                        maxLength={4000}
                        block={true}
                        minRows={3}
                    />
                    {decide.error && (
                        <Plate view='negative' title='Не получилось сохранить решение'>
                            {stale ? (
                                <div className='decision-panel__error'>
                                    Заявку изменили — досье обновлено. Подставьте данные заново или отправьте как есть.
                                    <div>
                                        <Button view='secondary' size={32} onClick={refill}>
                                            Заполнить по новым данным
                                        </Button>
                                    </div>
                                </div>
                            ) : fieldErrors.length ? (
                                <ul className='decision-panel__errors'>
                                    {fieldErrors.map((e) => (
                                        <li key={`${e.field}:${e.message}`}>{e.message}</li>
                                    ))}
                                </ul>
                            ) : (
                                decide.error.message
                            )}
                        </Plate>
                    )}
                </div>
            </SidePanelResponsive.Content>
            <SidePanelResponsive.Footer>
                <SidePanelResponsive.Controls
                    primary={
                        <Button view={mode === 'REJECT' ? 'secondary' : 'primary'} size={56} onClick={submit} loading={decide.isPending}>
                            {submitLabel}
                        </Button>
                    }
                    secondary={
                        <Button view='secondary' size={56} onClick={onClose}>
                            Отмена
                        </Button>
                    }
                />
            </SidePanelResponsive.Footer>
        </SidePanelResponsive>
    );
}
