import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Badge,
  Collapse,
  Divider,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  ListSubheader,
  Tooltip,
} from '@mui/material';
import ExpandLessIcon from '@mui/icons-material/ExpandLess';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import { useAuth } from '../../contexts/AuthContext';
import { useTeamFilter } from '../../contexts/TeamFilterContext';
import { useSettingsStatus } from '../../hooks/useSettingsStatus';
import { isItemActive, visibleGroups, NavGroup, NavItem } from './navConfig';

const STORAGE_KEY_GROUPS = 'kh.nav.groups';

const readGroupState = (): Record<string, boolean> => {
  try {
    const raw = localStorage.getItem(STORAGE_KEY_GROUPS);
    return raw ? (JSON.parse(raw) as Record<string, boolean>) : {};
  } catch {
    return {};
  }
};

interface NavListProps {
  /** Drawer expanded (labels visible) or rail (icons only). */
  open: boolean;
}

/**
 * Sidebar navigation driven by `navConfig`. Headed groups collapse and the
 * expansion state persists; the group holding the current page is always
 * expanded so the active item is never hidden. In rail mode headers become
 * dividers and every item shows its label in a tooltip.
 */
const NavList: React.FC<NavListProps> = ({ open }) => {
  const { t } = useTranslation('navigation');
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const { userRole } = useAuth();
  const { teamsEnabled } = useTeamFilter();
  const { attentionCount } = useSettingsStatus();

  const groups = useMemo(
    () => visibleGroups({ isAdmin: userRole === 'admin', teamsEnabled }),
    [userRole, teamsEnabled]
  );

  const [expanded, setExpanded] = useState<Record<string, boolean>>(readGroupState);
  useEffect(() => {
    try {
      localStorage.setItem(STORAGE_KEY_GROUPS, JSON.stringify(expanded));
    } catch {
      /* private mode */
    }
  }, [expanded]);

  const toggleGroup = useCallback((id: string) => {
    setExpanded((prev) => ({ ...prev, [id]: !(prev[id] ?? true) }));
  }, []);

  const renderItem = (item: NavItem, indent: boolean) => {
    const active = isItemActive(item, pathname);
    const label = t(item.labelKey) as string;
    const badgeCount = item.badge === 'settingsAttention' ? attentionCount : 0;
    const icon = badgeCount > 0 ? (
      <Badge color="warning" badgeContent={badgeCount} max={9}>
        {item.icon}
      </Badge>
    ) : (
      item.icon
    );
    const button = (
      <ListItemButton
        key={item.id}
        onClick={() => navigate(item.path)}
        selected={active}
        aria-current={active ? 'page' : undefined}
        sx={{
          minHeight: 44,
          justifyContent: open ? 'initial' : 'center',
          px: 2.5,
          pl: open && indent ? 3.5 : 2.5,
        }}
      >
        <ListItemIcon sx={{ minWidth: 0, mr: open ? 2.5 : 'auto', justifyContent: 'center' }}>{icon}</ListItemIcon>
        <ListItemText primary={label} sx={{ opacity: open ? 1 : 0, whiteSpace: 'nowrap' }} />
      </ListItemButton>
    );
    if (open) return button;
    return (
      <Tooltip key={item.id} title={label} placement="right">
        {button}
      </Tooltip>
    );
  };

  const renderGroup = (group: NavGroup, index: number) => {
    const containsActive = group.items.some((i) => isItemActive(i, pathname));
    const isOpen = !group.collapsible || containsActive || (expanded[group.id] ?? true);
    const items = group.items.map((i) => renderItem(i, Boolean(group.labelKey) && open));

    if (!group.labelKey) {
      return (
        <React.Fragment key={group.id}>
          {index > 0 && <Divider sx={{ my: 0.5 }} />}
          <List disablePadding>{items}</List>
        </React.Fragment>
      );
    }

    if (!open) {
      return (
        <React.Fragment key={group.id}>
          <Divider sx={{ my: 0.5 }} />
          <List disablePadding>{items}</List>
        </React.Fragment>
      );
    }

    const label = t(group.labelKey) as string;
    return (
      <List
        key={group.id}
        disablePadding
        subheader={
          <ListSubheader
            component="div"
            disableSticky
            onClick={group.collapsible ? () => toggleGroup(group.id) : undefined}
            aria-expanded={group.collapsible ? isOpen : undefined}
            role={group.collapsible ? 'button' : undefined}
            sx={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              lineHeight: '32px',
              mt: 0.5,
              pr: 1.5,
              cursor: group.collapsible ? 'pointer' : 'default',
              userSelect: 'none',
              bgcolor: 'transparent',
              fontSize: 11,
              fontWeight: 700,
              letterSpacing: '0.08em',
              textTransform: 'uppercase',
              color: 'text.secondary',
            }}
          >
            {label}
            {group.collapsible &&
              (isOpen ? <ExpandLessIcon fontSize="small" /> : <ExpandMoreIcon fontSize="small" />)}
          </ListSubheader>
        }
      >
        <Collapse in={isOpen} timeout="auto" unmountOnExit>
          {items}
        </Collapse>
      </List>
    );
  };

  return (
    <nav aria-label={t('aria.mainNavigation') as string}>
      {groups.map(renderGroup)}
    </nav>
  );
};

export default NavList;
