import { useState } from 'react';
import { useNavigate } from 'react-router-dom';

import type { CollectorApplication } from '../../api/collector/types';
import type { Product } from '../../api/types';
import type { Draft, Errors } from './draft';
import type { StepId } from './steps';

export type StepProps = {
    userId: string;
    app: CollectorApplication;
    product?: Product;
    draft: Draft;
    hrefFor: (step: StepId) => string;
    /** Ошибки шага по нашим проверкам (те же правила, что у data-collector). */
    errors: Errors;
    /** Ошибки из последнего ответа 422 data-collector, по полю. */
    serverErrors: Errors;
    /** Не директор (у ИП — не сам предприниматель): договор по доверенности. */
    needsPoa: boolean;
    soleProprietor: boolean;
};

/** «Продолжить» с проверкой: ошибки полей показываем после первой попытки уйти с шага. */
export function useStepGate(errors: Errors, serverErrors: Errors, nextHref: string) {
    const navigate = useNavigate();
    const [tried, setTried] = useState(false);
    const invalid = Object.keys(errors).length > 0;
    return {
        errorOf: (field: string) => (tried ? errors[field] : undefined) ?? serverErrors[field],
        onNext: () => {
            if (invalid) setTried(true);
            else navigate(nextHref);
        },
        hint: tried && invalid ? 'Исправьте поля, отмеченные красным' : undefined,
    };
}
