import { type ReactNode, useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';
import { Spinner } from '@alfalab/core-components/spinner';
import { Link, Navigate, useNavigate, useParams, useSearchParams } from 'react-router-dom';

import { ApiError } from '../../api/client';
import { useApplicants, useApplication, useDocuments, useProducts } from '../../api/queries';
import type { Applicant, Application, Me, Product, PurposeCode } from '../../api/types';
import { fieldLabel, PURPOSES } from '../../shared/lib/dictionaries';
import { mergePatch } from '../../shared/lib/mergePatch';
import { validTerms } from '../../shared/lib/terms';
import { WizardHeader } from './components/WizardHeader';
import { WizardNav } from './components/WizardNav';
import type { StepProps } from './stepProps';
import { firstIncompleteStep, isStepId, type StepId, stepState } from './steps';
import { CollateralStep } from './steps/CollateralStep';
import { CompanyStep } from './steps/CompanyStep';
import { DebtsStep } from './steps/DebtsStep';
import { DocumentsStep } from './steps/DocumentsStep';
import { FinanceStep } from './steps/FinanceStep';
import { ReviewStep } from './steps/ReviewStep';
import { TermsStep } from './steps/TermsStep';
import { unsavedLeftover, useDraft } from './useDraft';
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

/** /applications/new?product=&amount=&term=&purpose= — шаг «Компания», черновик создаётся по «Продолжить». */
export function NewApplicationPage({ me }: { me: Me }) {
    const navigate = useNavigate();
    const [params] = useSearchParams();
    const products = useProducts();
    const productCode = params.get('product');
    const product = products.data?.find((p) => p.code === productCode);
    const purposeParam = params.get('purpose') as PurposeCode | null;
    const purpose = purposeParam && PURPOSES.includes(purposeParam) ? purposeParam : undefined;
    const requested = validTerms(product, {
        amount: Number(params.get('amount')) || '',
        term: Number(params.get('term')) || '',
    });

    if (!productCode || (products.isSuccess && !product)) return <Navigate to='/products' replace={true} />;

    return (
        <WizardShell
            header={<WizardHeader exitLabel='Выйти' onExit={() => navigate('/products')} />}
            nav={
                <WizardNav
                    productName={product?.name ?? 'Кредит'}
                    subtitle='Новая заявка'
                    percent={0}
                    current='company'
                    stateOf={() => 'todo'}
                />
            }
        >
            <CompanyStep mode='create' me={me} productCode={productCode} requested={requested} purpose={purpose} />
        </WizardShell>
    );
}

// ---------------------------------------------------------------- черновик

/** /applications/:applicationId/edit/:step — анкета существующего черновика. */
export function WizardPage({ me }: { me: Me }) {
    const { applicationId, step } = useParams();
    const navigate = useNavigate();
    const application = useApplication(applicationId);
    const products = useProducts();
    const applicants = useApplicants();

    // ошибка фонового перечитывания при данных в кэше не должна сносить форму
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
    // шагам нужен продукт уже при первом рендере (диапазоны суммы и срока)
    if (!app || products.isPending) {
        return (
            <div className='wizard wizard_loading'>
                <Spinner visible={true} preset={48} />
            </div>
        );
    }

    // отправленную заявку не редактируют — показываем её статус
    if (!app.allowed_actions.includes('EDIT')) return <Navigate to={`/applications/${app.id}`} replace={true} />;
    if (!isStepId(step)) return <Navigate to={`/applications/${app.id}/edit/${firstIncompleteStep(app)}`} replace={true} />;

    // несохранённое с прошлого раза: шаги монтируются уже с ним, а useDraft досохранит
    const leftover = unsavedLeftover(app.id);
    return (
        <EditWizard
            key={app.id}
            me={me}
            app={leftover ? mergePatch(app, leftover) : app}
            step={step}
            product={products.data?.find((p) => p.code === app.product_code)}
            applicant={applicants.data?.find((a) => a.id === app.applicant.id)}
        />
    );
}

type EditProps = { me: Me; app: Application; step: StepId; product?: Product; applicant?: Applicant };

function EditWizard({ me, app, step, product, applicant }: EditProps) {
    const navigate = useNavigate();
    const draft = useDraft(app);
    const documents = useDocuments(app.id);
    const [exiting, setExiting] = useState(false);
    const hrefFor = (s: StepId) => `/applications/${app.id}/edit/${s}`;

    const exit = async () => {
        setExiting(true);
        try {
            await draft.flush();
            navigate('/');
        } catch {
            setExiting(false);
        }
    };

    const props: StepProps = { app, product, draft, hrefFor };
    const content = {
        company: <CompanyStep mode='edit' me={me} applicant={applicant} nextHref={hrefFor('terms')} />,
        terms: <TermsStep {...props} />,
        finance: <FinanceStep {...props} />,
        debts: <DebtsStep {...props} />,
        collateral: <CollateralStep {...props} />,
        documents: <DocumentsStep {...props} />,
        review: <ReviewStep {...props} />,
    }[step];

    return (
        <WizardShell
            header={
                <WizardHeader
                    status={draft.status}
                    savedAt={draft.savedAt}
                    onRetry={() => void draft.flush().catch(() => undefined)}
                    exitLabel='Сохранить и выйти'
                    onExit={exit}
                    exiting={exiting}
                />
            }
            nav={
                <WizardNav
                    productName={product?.name ?? 'Кредит'}
                    subtitle={`Заявка ${app.number}`}
                    percent={app.completeness.percent}
                    current={step}
                    hrefFor={hrefFor}
                    stateOf={(s) => stepState(s, app, product, documents.data)}
                />
            }
        >
            <div className='wizard__notices'>
                {draft.rejected.length > 0 && (
                    <Plate view='negative' title='Эти данные не сохранились'>
                        {draft.rejected.map((r) => `${fieldLabel(r.field)}: ${r.message}`).join('; ')}
                    </Plate>
                )}
                {draft.conflict && (
                    <Plate view='attention' title='Заявку меняли в другой вкладке'>
                        Мы взяли свежую версию и сохранили ваши правки поверх. Проверьте данные на шаге «Проверка и отправка».
                    </Plate>
                )}
            </div>
            <div key={step}>{content}</div>
        </WizardShell>
    );
}
