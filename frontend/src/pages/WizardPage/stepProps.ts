import type { Application, Product } from '../../api/types';
import type { StepId } from './steps';
import type { Draft } from './useDraft';

export type StepProps = {
    app: Application;
    product?: Product;
    draft: Draft;
    hrefFor: (step: StepId) => string;
};
