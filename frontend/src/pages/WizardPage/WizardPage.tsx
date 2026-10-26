import { type ReactNode, useEffect } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';
import { Spinner } from '@alfalab/core-components/spinner';
import { Link, Navigate, useNavigate, useParams, useSearchParams } from 'react-router-dom';

import { ApiError } from '../../api/client';
import { useCollectorApplication } from '../../api/collector/queries';
import { type CollectorApplication, type CollectorPurpose, signsWithoutPoa } from '../../api/collector/types';
import { useProducts } from '../../api/queries';
import type { Me, Product } from '../../api/types';
import { PURPOSES } from '../../shared/lib/dictionaries';
import { useSessionState } from '../../shared/lib/sessionState';
import { validTerms } from '../../shared/lib/terms';
import { WizardHeader } from './components/WizardHeader';
import { WizardNav } from './components/WizardNav';
import { discardDraft, type Errors, type QuestionnaireDraft, type Secrets, serverErrorsKey, useLocalDraft } from './draft';
import type { StepProps } from './stepProps';
import { completionPercent, errorsOf, firstIncompleteStep, isStepId, STEPS, type StepId, stepState } from './steps';
import { BorrowerStep } from './steps/BorrowerStep';
import { CollateralStep } from './steps/CollateralStep';
import { CompanyStep } from './steps/CompanyStep';
import { DocumentsStep } from './steps/DocumentsStep';
import { ReviewStep } from './steps/ReviewStep';
import { ScoringStep } from './steps/ScoringStep';
import { SourcesStep } from './steps/SourcesStep';
import { TermsStep } from './steps/TermsStep';
import './WizardPage.css';

function WizardShell({ header, nav, children }: { header: ReactNode; nav?: ReactNode; children: ReactNode }) {
    return (
        <div className='wizard'>
            {header}
            <div className='wizard__body'>
                {nav}
                <main className='wizard__content'>{children}</main>
            </div>
        </div>
    );
}

// ---------------------------------------------------------------- новая заявка

/** /applications/new?product=&amount=&term=&purpose= — шаг «Компания»: регистрация в data-collector создаёт черновик. */
export function NewApplicationPage({ me }: { me: Me }) {
    const navigate = useNavigate();
    const [params] = useSearchParams();
    const products = useProducts();
    const productCode = params.get('product');
    const product = products.data?.find((p) => p.code === productCode);
    const purposeParam = params.get('purpose') as CollectorPurpose | null;
    const purpose = purposeParam && (PURPOSES as string[]).includes(purposeParam) ? purposeParam : undefined;
    const requested = validTerms(product, {
        amount: Number(params.get('amount')) || '',
        term: Number(params.get('term')) || '',
    });

    if (!productCode || (products.isSuccess && !product)) return <Navigate to='/products' replace={true} />;

    return (
        <WizardShell
            header={<WizardHeader exitLabel='Выйти' onExit={() => navigate('/products')} />}
            nav={<WizardNav productName={product?.name ?? 'Кредит'} subtitle='Новая заявка' percent={0} current='company' stateOf={() => 'todo'} />}
        >
            <CompanyStep mode='create' me={me} productCode={productCode} requested={requested} purpose={purpose} />
        </WizardShell>
    );
}

// ---------------------------------------------------------------- черновик

/** /applications/:applicationId/edit/:step — анкета черновика data-collector. */
export function WizardPage({ me }: { me: Me }) {
    const { applicationId, step } = useParams();
    const navigate = useNavigate();
    const application = useCollectorApplication(applicationId);
    const products = useProducts();

    if (application.isError && !application.data) {
        const notFound = application.error instanceof ApiError && application.error.status === 404;
        return (
            <WizardShell header={<WizardHeader exitLabel='На главную' onExit={() => navigate('/')} />}>
                <Plate view='negative' title={notFound ? 'Заявка не найдена' : 'Не удалось загрузить заявку'}>
                    <Button Component={Link} href='/' view='secondary' size={40}>
                        На главную
                    </Button>
                </Plate>
            </WizardShell>
        );
    }

    const app = application.data;
    if (!app || products.isPending) {
        return (
            <div className='wizard wizard_loading'>
                <Spinner visible={true} preset={48} />
            </div>
        );
    }
    // анкету принимают один раз — отправленную (в том числе из другой вкладки) показываем статусом
    if (app.status !== 'DRAFT') return <SubmittedRedirect userId={me.id} id={app.id ?? ''} />;

    return <EditWizard key={app.id} userId={me.id} app={app} step={isStepId(step) ? step : undefined} products={products.data ?? []} />;
}

/** Анкету уже отправили: локальный черновик, токены и файлы больше не нужны. */
function SubmittedRedirect({ userId, id }: { userId: string; id: string }) {
    useEffect(() => discardDraft(userId, id), [userId, id]);
    return <Navigate to={`/applications/${id}`} replace={true} />;
}

/** Корень поля из Problem.errors[].field: «director.inn» → «director», «files[2]» → «files». */
const rootOf = (field: string) => field.split(/[.[]/)[0] ?? field;

type Rejected = { errors: Errors; values: Record<string, string> };

/** Значение корня поля в черновике — чтобы понять, исправили ли его после ответа сервера. */
function snapshotOf(root: string, d: QuestionnaireDraft, secrets: Secrets) {
    const source: Record<string, unknown> = {
        ...d,
        director: d.with_director ? d.director : undefined,
        documents: d.files,
        files: d.files,
        marketplace_tokens: secrets.marketplace_tokens,
        ofd_token: secrets.ofd_token,
    };
    return JSON.stringify(source[root] ?? null);
}

function EditWizard({ userId, app, step, products }: { userId: string; app: CollectorApplication; step?: StepId; products: Product[] }) {
    const navigate = useNavigate();
    const id = app.id ?? '';
    const { amount, term_months } = app.requested ?? {};
    const draft = useLocalDraft(userId, id, {
        product_code: app.product_code,
        requested: amount !== undefined && term_months !== undefined ? { amount, term_months } : undefined,
        purpose: app.purpose,
    });
    // ошибки 422 последней отправки и значения полей на тот момент; переживают переход между шагами
    const [rejected, setRejected] = useSessionState<Rejected>(serverErrorsKey(userId, id), () => ({ errors: {}, values: {} }));
    const hrefFor = (s: StepId) => `/applications/${id}/edit/${s}`;
    const valueOf = (root: string) => snapshotOf(root, draft.value, draft.secrets);
    // поле поменяли — его серверная ошибка устарела (и у списков: строки могли сдвинуться)
    const serverErrors = Object.fromEntries(Object.entries(rejected.errors).filter(([field]) => rejected.values[rootOf(field)] === valueOf(rootOf(field))));
    const onServerErrors = (errors: Errors) =>
        setRejected({ errors, values: Object.fromEntries(Object.keys(errors).map((field) => [rootOf(field), valueOf(rootOf(field))])) });

    const soleProprietor = app.applicant?.type === 'SOLE_PROPRIETOR';
    // кто подписывает без доверенности, решает data-collector (can_sign)
    const needsPoa = app.representative?.can_sign === undefined ? !signsWithoutPoa(app.representative?.role, soleProprietor) : !app.representative.can_sign;
    const d = draft.value;
    const product = products.find((p) => p.code === d.product_code);

    if (!step) return <Navigate to={hrefFor(firstIncompleteStep(d, draft.secrets, needsPoa))} replace={true} />;

    // ошибку поля убираем, как только поле поменяли: в черновике уже другое значение
    const errors = errorsOf(step, d, draft.secrets, needsPoa);
    const stepServerErrors = Object.fromEntries(Object.entries(serverErrors).filter(([field]) => STEPS.find((s) => s.id === step)?.owns?.(field)));
    const props: StepProps = { userId, app, product, draft, hrefFor, errors, serverErrors: stepServerErrors, needsPoa, soleProprietor };

    const content = {
        company: <CompanyStep mode='edit' app={app} nextHref={hrefFor('terms')} />,
        terms: <TermsStep {...props} />,
        borrower: <BorrowerStep {...props} />,
        scoring: <ScoringStep {...props} />,
        sources: <SourcesStep {...props} />,
        collateral: <CollateralStep {...props} />,
        documents: <DocumentsStep {...props} />,
        review: <ReviewStep {...props} serverErrors={serverErrors} onServerErrors={onServerErrors} />,
    }[step];

    return (
        <WizardShell
            header={
                <WizardHeader
                    status={draft.savedAt ? 'saved' : 'idle'}
                    savedAt={draft.savedAt}
                    exitLabel='Сохранить и выйти'
                    onExit={() => navigate('/')}
                />
            }
            nav={
                <WizardNav
                    productName={product?.name ?? 'Кредит'}
                    subtitle={`Заявка ${app.number ?? ''}`}
                    percent={completionPercent(d, draft.secrets, needsPoa)}
                    current={step}
                    hrefFor={hrefFor}
                    stateOf={(s) => (Object.keys(serverErrors).some((f) => STEPS.find((x) => x.id === s)?.owns?.(f)) ? 'todo' : stepState(s, d, draft.secrets, needsPoa))}
                />
            }
        >
            <div key={step}>{content}</div>
        </WizardShell>
    );
}
