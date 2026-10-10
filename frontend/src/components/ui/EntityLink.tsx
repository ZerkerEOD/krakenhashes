import React from 'react';
import { Link as RouterLink } from 'react-router-dom';
import { Link, LinkProps, Typography } from '@mui/material';
import { useAuth } from '../../contexts/AuthContext';
import { EntityRouteContext, EntityType, entityRoute, isAdminRoute } from '../../constants/routes';

export interface EntityLinkProps extends Omit<LinkProps, 'href' | 'component' | 'id'> {
  type: EntityType | string;
  id?: string | number | null;
  /** Visible text; defaults to `#id`. */
  label?: React.ReactNode;
  ctx?: EntityRouteContext;
  /** Stop the click from reaching row handlers (grid rows, list items). Default true. */
  stopPropagation?: boolean;
  /** Render in the monospace face (ids, hashes). */
  mono?: boolean;
  /** Force plain text even when a route exists. */
  disabled?: boolean;
  newTab?: boolean;
}

/**
 * A link to another entity. Renders plain text instead of a link when the
 * target lives under /admin and the viewer is not an admin, or when there is
 * no id to link to.
 */
const EntityLink: React.FC<EntityLinkProps> = ({
  type,
  id,
  label,
  ctx,
  stopPropagation = true,
  mono,
  disabled,
  newTab,
  sx,
  onClick,
  children,
  ...rest
}) => {
  const { userRole } = useAuth();
  const text = children ?? label ?? (id !== undefined && id !== null ? `#${id}` : '—');
  const hasId = id !== undefined && id !== null && id !== '';
  const needsId = !['cloud_instance', 'wordlist', 'rule', 'binary', 'security', 'webhook', 'pot'].includes(String(type));
  const to = entityRoute(type, id, ctx);
  const blocked = disabled || (needsId && !hasId) || (isAdminRoute(to) && userRole !== 'admin');

  const monoSx = mono ? { fontFamily: (t: any) => t.typography.monoFamily } : {};

  if (blocked) {
    return (
      <Typography component="span" sx={{ ...monoSx, ...(sx as any) }} variant="inherit">
        {text}
      </Typography>
    );
  }

  return (
    <Link
      component={RouterLink}
      to={to}
      underline="hover"
      color="primary"
      target={newTab ? '_blank' : undefined}
      rel={newTab ? 'noopener noreferrer' : undefined}
      onClick={(e: React.MouseEvent<HTMLAnchorElement>) => {
        if (stopPropagation) e.stopPropagation();
        onClick?.(e);
      }}
      sx={{ fontWeight: 500, ...monoSx, ...(sx as any) }}
      {...rest}
    >
      {text}
    </Link>
  );
};

export default EntityLink;
