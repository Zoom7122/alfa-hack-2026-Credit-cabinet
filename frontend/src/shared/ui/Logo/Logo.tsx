import { AlfaBankLogoMIcon } from '@alfalab/icons-logotype/AlfaBankLogoMIcon';

import './Logo.css';

type Props = { inverted?: boolean };

export function Logo({ inverted }: Props) {
    return (
        <span className={`logo${inverted ? ' logo_inverted' : ''}`} aria-hidden='true'>
            <AlfaBankLogoMIcon className='logo__sign' />
        </span>
    );
}
