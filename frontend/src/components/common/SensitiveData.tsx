/**
 * SensitiveData - Wrapper component to protect sensitive data from translation
 *
 * This component prevents browser translation services from translating
 * sensitive data like passwords, hashes, usernames, and credentials.
 *
 * It applies:
 * - translate="no" HTML attribute (standard)
 * - className="notranslate" (Google Translate specific)
 * - lang="en" attribute (hints content is technical/English)
 *
 * Usage:
 * ```tsx
 * <SensitiveData>{hash.password}</SensitiveData>
 * <SensitiveData as="code">{hash.original_hash}</SensitiveData>
 * <SensitiveData as="td">{row.username}</SensitiveData>
 * ```
 *
 * @param {SensitiveDataProps} props - Component props
 * @returns {JSX.Element} Protected wrapper element
 */

import React from 'react';

type AllowedElements =
    | 'span'
    | 'div'
    | 'code'
    | 'pre'
    | 'td'
    | 'p'
    | 'strong'
    | 'em';

interface SensitiveDataProps {
    /** Content to protect from translation */
    children: React.ReactNode;
    /** HTML element to render (default: span) */
    as?: AllowedElements;
    /** Additional CSS class names */
    className?: string;
    /** Additional inline styles */
    style?: React.CSSProperties;
    /** onClick handler */
    onClick?: (event: React.MouseEvent) => void;
}

const SensitiveData: React.FC<SensitiveDataProps> = ({
    children,
    as: Component = 'span',
    className = '',
    style,
    onClick,
}) => {
    // Combine notranslate with any additional classes
    const combinedClassName = `notranslate ${className}`.trim();

    return React.createElement(
        Component,
        {
            translate: 'no',
            className: combinedClassName,
            lang: 'en',
            style,
            onClick,
        },
        children
    );
};

export default SensitiveData;
