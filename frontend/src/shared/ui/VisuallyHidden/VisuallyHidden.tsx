import type { HTMLAttributes, ReactNode } from 'react';

import './VisuallyHidden.css';

/** Текст только для скринридера: не виден, но читается. */
export function VisuallyHidden({ children, ...rest }: { children: ReactNode } & HTMLAttributes<HTMLSpanElement>) {
    return (
        <span {...rest} className='visually-hidden'>
            {children}
        </span>
    );
}
