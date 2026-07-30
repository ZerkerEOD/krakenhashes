/**
 * BloodhoundSection renders the Active Directory privilege-exposure analytics produced when a
 * BloodHound collection dump is uploaded with a report. It cross-references cracked accounts against
 * their AD standing (Domain Admin, Kerberoastable, DCSync, local-admin reach, path to Domain Admin).
 *
 * These sections are forest-wide and appear only on the top-level ("All") report data. The component
 * renders nothing when no dump was uploaded.
 */
import React from 'react';
import { useTranslation } from 'react-i18next';
import {
  Card,
  CardContent,
  Typography,
  Box,
  Chip,
  Grid,
  Alert,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Divider,
} from '@mui/material';
import { Security as SecurityIcon } from '@mui/icons-material';
import { AnalyticsData, CompromisedAccount } from '../../types/analytics';

interface Props {
  data: AnalyticsData;
}

const pct = (v: number): string => `${(v ?? 0).toFixed(2)}%`;
const totalOrNA = (v: number, na: string): string => (v < 0 ? na : `${v}`);

const StatRow: React.FC<{ label: string; value: React.ReactNode }> = ({ label, value }) => (
  <Box sx={{ display: 'flex', justifyContent: 'space-between', py: 0.5 }}>
    <Typography variant="body2" color="text.secondary">
      {label}
    </Typography>
    <Typography variant="body2" fontWeight={600}>
      {value}
    </Typography>
  </Box>
);

const SubCard: React.FC<{ title: string; critical?: boolean; children: React.ReactNode }> = ({
  title,
  critical,
  children,
}) => (
  <Card variant="outlined" sx={{ height: '100%', borderColor: critical ? 'error.main' : undefined }}>
    <CardContent>
      <Typography variant="subtitle1" gutterBottom fontWeight={600}>
        {title}
      </Typography>
      {children}
    </CardContent>
  </Card>
);

const AccountsTable: React.FC<{
  accounts?: CompromisedAccount[];
  extraLabel?: string;
  extraValue?: (a: any) => React.ReactNode;
  maxRows?: number;
}> = ({ accounts, extraLabel, extraValue, maxRows = 100 }) => {
  const { t } = useTranslation('analytics');
  if (!accounts || accounts.length === 0) return null;
  const rows = accounts.slice(0, maxRows);
  return (
    <TableContainer sx={{ mt: 1, maxHeight: 360 }}>
      <Table size="small" stickyHeader>
        <TableHead>
          <TableRow>
            <TableCell>{t('columns.username')}</TableCell>
            <TableCell>{t('columns.domain')}</TableCell>
            <TableCell>{t('bloodhound.privilegedGroups')}</TableCell>
            {extraLabel && <TableCell align="right">{extraLabel}</TableCell>}
          </TableRow>
        </TableHead>
        <TableBody>
          {rows.map((a, i) => (
            <TableRow key={`${a.sid || a.username}-${i}`}>
              <TableCell>{a.username}</TableCell>
              <TableCell>{a.domain || '—'}</TableCell>
              <TableCell>
                {a.privileged_groups && a.privileged_groups.length > 0
                  ? a.privileged_groups.join(', ')
                  : '—'}
              </TableCell>
              {extraLabel && <TableCell align="right">{extraValue ? extraValue(a) : ''}</TableCell>}
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {accounts.length > rows.length && (
        <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: 'block' }}>
          {t('bloodhound.showingAccounts', { shown: rows.length, total: accounts.length })}
        </Typography>
      )}
    </TableContainer>
  );
};

const BloodhoundSection: React.FC<Props> = ({ data }) => {
  const { t } = useTranslation('analytics');
  const {
    ad_privilege,
    path_to_domain_admin,
    kerberoast_cracked,
    asrep_roast_cracked,
    admin_count_cracked,
    local_admin_blast_radius,
    dcsync_cracked,
  } = data;

  const present =
    ad_privilege ||
    path_to_domain_admin ||
    kerberoast_cracked ||
    asrep_roast_cracked ||
    admin_count_cracked ||
    local_admin_blast_radius ||
    dcsync_cracked;
  if (!present) return null;

  const na = t('bloodhound.notApplicable');
  const daCritical = (ad_privilege?.cracked_effective_domain_admin ?? 0) > 0;
  const dcsyncCritical = (dcsync_cracked?.cracked ?? 0) > 0;
  const pathCritical = (path_to_domain_admin?.cracked_with_path ?? 0) > 0;

  return (
    <Card sx={{ mb: 3 }}>
      <CardContent>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
          <SecurityIcon color="error" />
          <Typography variant="h6">{t('bloodhound.title')}</Typography>
        </Box>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('bloodhound.description')}
        </Typography>

        {(daCritical || dcsyncCritical || pathCritical) && (
          <Alert severity="error" sx={{ mb: 2 }}>
            {daCritical && (
              <div>
                {t('bloodhound.alertEffectiveDA', {
                  n: ad_privilege!.cracked_effective_domain_admin,
                })}
              </div>
            )}
            {dcsyncCritical && (
              <div>{t('bloodhound.alertDCSync', { n: dcsync_cracked!.cracked })}</div>
            )}
            {pathCritical && !path_to_domain_admin!.skipped && (
              <div>
                {t('bloodhound.alertPath', {
                  n: path_to_domain_admin!.cracked_with_path,
                  hops: path_to_domain_admin!.shortest_hops,
                })}
              </div>
            )}
          </Alert>
        )}

        <Grid container spacing={2}>
          {ad_privilege && (
            <Grid item xs={12} md={6}>
              <SubCard title={t('bloodhound.privilegedCompromise')} critical={daCritical}>
                <StatRow
                  label={t('bloodhound.crackedPrivileged')}
                  value={
                    <>
                      {ad_privilege.cracked_privileged}
                      {daCritical && (
                        <Chip
                          size="small"
                          color="error"
                          label={t('bloodhound.daChip', {
                            n: ad_privilege.cracked_effective_domain_admin,
                          })}
                          sx={{ ml: 1 }}
                        />
                      )}
                    </>
                  }
                />
                <StatRow label={t('bloodhound.crackedTierZero')} value={ad_privilege.cracked_tier_zero} />
                <StatRow label={t('bloodhound.privilegedInScope')} value={ad_privilege.in_scope_privileged} />
                <StatRow label={t('bloodhound.privilegedCrackedRate')} value={pct(ad_privilege.percent_privileged_cracked)} />
                <StatRow label={t('bloodhound.privilegedInDomain')} value={totalOrNA(ad_privilege.domain_privileged_total, na)} />
                <AccountsTable accounts={ad_privilege.accounts} />
              </SubCard>
            </Grid>
          )}

          {path_to_domain_admin && (
            <Grid item xs={12} md={6}>
              <SubCard title={t('bloodhound.pathToDA')} critical={pathCritical}>
                {path_to_domain_admin.skipped ? (
                  <Alert severity="info">{t('bloodhound.pathSkipped')}</Alert>
                ) : (
                  <>
                    <StatRow label={t('bloodhound.crackedWithPath')} value={path_to_domain_admin.cracked_with_path} />
                    <StatRow label={t('bloodhound.inScopeWithPath')} value={path_to_domain_admin.in_scope_with_path} />
                    <StatRow label={t('bloodhound.shortestPath')} value={path_to_domain_admin.shortest_hops} />
                    <AccountsTable
                      accounts={path_to_domain_admin.accounts}
                      extraLabel={t('bloodhound.hops')}
                      extraValue={(a) => a.hops}
                    />
                  </>
                )}
              </SubCard>
            </Grid>
          )}

          {kerberoast_cracked && (kerberoast_cracked.cracked > 0 || kerberoast_cracked.in_scope_total > 0) && (
            <Grid item xs={12} md={6}>
              <SubCard title={t('bloodhound.kerberoastable')}>
                <StatRow label={t('columns.cracked')} value={t('bloodhound.crackedWithPrivileged', { cracked: kerberoast_cracked.cracked, privileged: kerberoast_cracked.cracked_privileged })} />
                <StatRow label={t('bloodhound.inScope')} value={kerberoast_cracked.in_scope_total} />
                <StatRow label={t('bloodhound.inDomain')} value={totalOrNA(kerberoast_cracked.domain_total, na)} />
                <StatRow label={t('bloodhound.crackedRate')} value={pct(kerberoast_cracked.percent_cracked)} />
                <AccountsTable accounts={kerberoast_cracked.accounts} />
              </SubCard>
            </Grid>
          )}

          {asrep_roast_cracked && (asrep_roast_cracked.cracked > 0 || asrep_roast_cracked.in_scope_total > 0) && (
            <Grid item xs={12} md={6}>
              <SubCard title={t('bloodhound.asrepRoastable')}>
                <StatRow label={t('columns.cracked')} value={t('bloodhound.crackedWithPrivileged', { cracked: asrep_roast_cracked.cracked, privileged: asrep_roast_cracked.cracked_privileged })} />
                <StatRow label={t('bloodhound.inScope')} value={asrep_roast_cracked.in_scope_total} />
                <StatRow label={t('bloodhound.inDomain')} value={totalOrNA(asrep_roast_cracked.domain_total, na)} />
                <StatRow label={t('bloodhound.crackedRate')} value={pct(asrep_roast_cracked.percent_cracked)} />
                <AccountsTable accounts={asrep_roast_cracked.accounts} />
              </SubCard>
            </Grid>
          )}

          {admin_count_cracked && (admin_count_cracked.cracked > 0 || admin_count_cracked.in_scope_total > 0) && (
            <Grid item xs={12} md={6}>
              <SubCard title={t('bloodhound.adminCount')}>
                <StatRow label={t('columns.cracked')} value={admin_count_cracked.cracked} />
                <StatRow label={t('bloodhound.inScope')} value={admin_count_cracked.in_scope_total} />
                <StatRow label={t('bloodhound.inDomain')} value={totalOrNA(admin_count_cracked.domain_total, na)} />
                <StatRow label={t('bloodhound.crackedRate')} value={pct(admin_count_cracked.percent_cracked)} />
                <AccountsTable accounts={admin_count_cracked.accounts} />
              </SubCard>
            </Grid>
          )}

          {dcsync_cracked && (
            <Grid item xs={12} md={6}>
              <SubCard title={t('bloodhound.dcsyncCapable')} critical={dcsyncCritical}>
                <StatRow label={t('bloodhound.crackedWithDCSync')} value={dcsync_cracked.cracked} />
                <StatRow label={t('bloodhound.dcsyncPrincipalsInDomain')} value={totalOrNA(dcsync_cracked.domain_principals, na)} />
                <AccountsTable accounts={dcsync_cracked.accounts} />
              </SubCard>
            </Grid>
          )}

          {local_admin_blast_radius && local_admin_blast_radius.cracked_with_local_admin > 0 && (
            <Grid item xs={12} md={6}>
              <SubCard title={t('bloodhound.localAdminBlast')}>
                <StatRow label={t('bloodhound.crackedWithLocalAdmin')} value={local_admin_blast_radius.cracked_with_local_admin} />
                <StatRow label={t('bloodhound.totalAdminRelationships')} value={local_admin_blast_radius.total_admin_relationships} />
                <StatRow label={t('bloodhound.largestSingleReach')} value={local_admin_blast_radius.max_computers_single} />
                <Divider sx={{ my: 1 }} />
                <AccountsTable
                  accounts={local_admin_blast_radius.top_accounts}
                  extraLabel={t('bloodhound.computers')}
                  extraValue={(a) => a.computer_count}
                />
              </SubCard>
            </Grid>
          )}
        </Grid>
      </CardContent>
    </Card>
  );
};

export default BloodhoundSection;
