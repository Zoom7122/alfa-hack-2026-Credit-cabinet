import { useState } from 'react';
import { Plate } from '@alfalab/core-components/plate';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Tag } from '@alfalab/core-components/tag';
import { Typography } from '@alfalab/core-components/typography';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';

import { useApplicants, useApplications, useProducts } from '../../api/queries';
import type { CompanyAccess, PurposeCode } from '../../api/types';
import { ACTIVE_STAGES, PURPOSE_LABELS, PURPOSES } from '../../shared/lib/dictionaries';
import { fitTerms, type TermsValue } from '../../shared/lib/terms';
import { useRovingRadio } from '../../shared/lib/useRovingRadio';
import { Calculator } from './components/Calculator';
import { ProductCard } from './components/ProductCard';
import './ProductsPage.css';

const DEFAULT_TERMS: TermsValue = { amount: 5_000_000, term: 24 };

const isPurpose = (value: string | null): value is PurposeCode => PURPOSES.includes(value as PurposeCode);

export function ProductsPage({ company }: { company?: CompanyAccess }) {
    const navigate = useNavigate();
    const [params, setParams] = useSearchParams();
    const purposeParam = params.get('purpose');
    const purpose: PurposeCode = isPurpose(purposeParam) ? purposeParam : 'WORKING_CAPITAL';

    const applicants = useApplicants();
    const applicant = applicants.data?.find((a) => a.id === company?.applicant_id);
    // ждём тип компании, иначе каталог сначала пришёл бы без фильтра и сменился на глазах
    const catalog = useProducts({ purpose, applicantType: applicant?.applicant_type }, !company || !applicants.isPending);
    const applications = useApplications({ applicantId: company?.applicant_id });

    const [selectedCode, setSelectedCode] = useState<string>();
    // сумма и срок запоминаются вместе с продуктом, для которого их ввели
    const [termsState, setTermsState] = useState<{ code?: string; value: TermsValue }>({ value: DEFAULT_TERMS });

    const products = catalog.data ?? [];
    // по умолчанию — первый (самый подходящий) продукт под задачу
    const product = products.find((p) => p.code === selectedCode) ?? products[0];
    const existing = applications.data?.find((a) => a.product_code === product?.code && ACTIVE_STAGES.includes(a.client_stage));
    const hasActive = (code: string) => applications.data?.some((a) => a.product_code === code && ACTIVE_STAGES.includes(a.client_stage));

    // сменился продукт (выбрали другой или фильтр по задаче) — приводим к его диапазону
    const terms = product && termsState.code !== product.code ? fitTerms(product, termsState.value) : termsState.value;
    const setTerms = (value: TermsValue) => setTermsState({ code: product?.code, value });

    const { ref: radiosRef, onKeyDown } = useRovingRadio(products, (p) => setSelectedCode(p.code));

    const selectPurpose = (code: PurposeCode) => {
        setParams({ purpose: code }, { replace: true });
        setSelectedCode(undefined);
    };

    const apply = () => {
        if (!product || terms.amount === '' || terms.term === '') return;
        const query = new URLSearchParams({
            product: product.code,
            amount: String(terms.amount),
            term: String(terms.term),
            purpose,
        });
        navigate(`/applications/new?${query}`);
    };

    return (
        <div className='products'>
            <nav aria-label='Вы здесь' className='products__crumbs'>
                <Link to='/' className='products__crumb-link'>
                    <Typography.Text view='primary-small'>Кредиты</Typography.Text>
                </Link>
                <Typography.Text view='primary-small' color='secondary' aria-hidden='true'>
                    →
                </Typography.Text>
                <Typography.Text view='primary-small' color='secondary' aria-current='page'>
                    Новая заявка
                </Typography.Text>
            </nav>

            <div className='products__head'>
                <Typography.TitleResponsive tag='h1' view='large' font='system'>
                    Для чего нужны деньги?
                </Typography.TitleResponsive>
                <Typography.Text view='primary-large' color='secondary'>
                    Выберите задачу — покажем подходящие кредиты
                </Typography.Text>
            </div>

            <div className='products__purposes' role='group' aria-label='Задача'>
                {PURPOSES.map((code) => (
                    <Tag
                        key={code}
                        size={40}
                        view='filled'
                        checked={purpose === code}
                        aria-pressed={purpose === code}
                        onClick={() => selectPurpose(code)}
                    >
                        {PURPOSE_LABELS[code]}
                    </Tag>
                ))}
            </div>

            {!company && (
                <Plate view='common' title='Компанию добавите на первом шаге заявки'>
                    Понадобится только ИНН — реквизиты подтянем из реестра.
                </Plate>
            )}

            <div className='products__columns'>
                <div ref={radiosRef} className='products__list' role='radiogroup' aria-label='Кредитный продукт'>
                    {(catalog.isLoading || (company && applicants.isPending)) && [0, 1, 2].map((i) => <Skeleton key={i} visible={true} className='products__skeleton' />)}
                    {catalog.isError && (
                        <Plate view='negative' title='Не удалось загрузить продукты'>
                            Обновите страницу или попробуйте позже.
                        </Plate>
                    )}
                    {catalog.isSuccess && products.length === 0 && (
                        <Plate view='common' title='Под эту задачу пока нет кредитов'>
                            Выберите другую задачу — или напишите нам в чат, подберём вручную.
                        </Plate>
                    )}
                    {products.map((p, i) => (
                        <ProductCard
                            key={p.code}
                            product={p}
                            checked={p.code === product?.code}
                            recommended={i === 0}
                            hasActiveApplication={hasActive(p.code)}
                            onSelect={() => setSelectedCode(p.code)}
                            onKeyDown={onKeyDown(i)}
                        />
                    ))}
                </div>

                {product && (
                    <Calculator
                        product={product}
                        value={terms}
                        onChange={setTerms}
                        existing={existing}
                        onApply={apply}
                    />
                )}
            </div>
        </div>
    );
}
