import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  TableRow,
  TableCell,
  Typography,
  Chip,
  IconButton,
  Box,
  Link,
  Tooltip,
  Alert,
  Collapse,
} from '@mui/material';
import {
  Delete as DeleteIcon,
  Speed as SpeedIcon,
  People as PeopleIcon,
  Refresh as RefreshIcon,
  ExpandMore as ExpandMoreIcon,
  ExpandLess as ExpandLessIcon,
  Error as ErrorIcon,
  Info as InfoIcon,
} from '@mui/icons-material';
import { useNavigate } from 'react-router-dom';
import EditableCell from './EditableCell';
import DeleteConfirm from './DeleteConfirm';
import { JobSummary } from '../../types/jobs';
import { api } from '../../services/api';
import { formatters } from '../../utils/formatters';
import { calculateJobProgress, formatKeyspace, getKeyspaceTooltip } from '../../utils/jobProgress';
import LinearProgress from '@mui/material/LinearProgress';

interface JobRowProps {
  job: JobSummary;
  onJobUpdated?: () => void;
  isLastActiveJob?: boolean;
  isCompletedSection?: boolean;
  maxPriority?: number;
}

const JobRow: React.FC<JobRowProps> = ({ job, onJobUpdated, isLastActiveJob, isCompletedSection, maxPriority = 10 }) => {
  const { t } = useTranslation('jobs');
  const navigate = useNavigate();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [isRetrying, setIsRetrying] = useState(false);
  const [showError, setShowError] = useState(false);

  const handleJobNameClick = () => {
    navigate(`/jobs/${job.id}`);
  };

  const handleUpdatePriority = async (newPriority: number) => {
    try {
      await api.patch(`/api/jobs/${job.id}`, { priority: newPriority });
      onJobUpdated?.();
    } catch (error) {
      console.error('Failed to update job priority:', error);
      throw error; // Re-throw to show error in EditableCell
    }
  };

  const handleUpdateMaxAgents = async (newMaxAgents: number) => {
    try {
      await api.patch(`/api/jobs/${job.id}`, { max_agents: newMaxAgents });
      onJobUpdated?.();
    } catch (error) {
      console.error('Failed to update max agents:', error);
      throw error; // Re-throw to show error in EditableCell
    }
  };

  const handleDeleteJob = async () => {
    setIsDeleting(true);
    try {
      await api.delete(`/api/jobs/${job.id}`);
      setDeleteDialogOpen(false);
      onJobUpdated?.();
    } catch (error) {
      console.error('Failed to delete job:', error);
    } finally {
      setIsDeleting(false);
    }
  };

  const handleRetryJob = async () => {
    setIsRetrying(true);
    try {
      await api.post(`/api/jobs/${job.id}/retry`);
      onJobUpdated?.();
    } catch (error) {
      console.error('Failed to retry job:', error);
    } finally {
      setIsRetrying(false);
    }
  };

  const getStatusColor = (status: string) => {
    switch (status.toLowerCase()) {
      case 'running':
        return 'success';
      case 'pending':
        return 'warning';
      case 'completed':
        return 'info';
      case 'failed':
        return 'error';
      case 'paused':
        return 'default';
      case 'cancelled':
        return 'default';
      default:
        return 'default';
    }
  };

  const canRetry = ['failed', 'cancelled'].includes(job.status.toLowerCase());
  const hasError = job.error_message && job.status === 'failed';

  // Format completion time if available
  const completionTime = job.completed_at ? new Date(job.completed_at).toLocaleString() : null;

  return (
    <>
      <TableRow 
        hover
        sx={{ 
          bgcolor: isCompletedSection ? 'action.selected' : 'inherit',
          borderBottom: isLastActiveJob ? '2px solid' : undefined,
          borderBottomColor: isLastActiveJob ? 'divider' : undefined
        }}>
        {/* Job Name with expand button for errors */}
        <TableCell>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            {hasError && (
              <IconButton
                size="small"
                onClick={() => setShowError(!showError)}
                sx={{ p: 0.5 }}
              >
                {showError ? <ExpandLessIcon /> : <ExpandMoreIcon />}
              </IconButton>
            )}
            <Link
              component="button"
              variant="body2"
              onClick={handleJobNameClick}
              sx={{ textAlign: 'left', fontWeight: 'medium' }}
            >
              {job.name}
            </Link>
            <Chip
              label={t(`status.${job.status}`) as string}
              color={getStatusColor(job.status)}
              size="small"
              variant="outlined"
              icon={hasError ? <ErrorIcon /> : undefined}
            />
          </Box>
        </TableCell>

        {/* Hashlist */}
        <TableCell>
          <Box>
            <Typography variant="body2">{job.hashlist_name}</Typography>
            {completionTime && (
              <Typography variant="caption" color="text.secondary">
                {t('row.completed')} {completionTime}
              </Typography>
            )}
          </Box>
        </TableCell>

        {/* Created By */}
        <TableCell>
          <Typography variant="body2" color="text.secondary">
            {job.created_by_username || t('row.unknown')}
          </Typography>
        </TableCell>

        {/* Progress */}
        <TableCell align="center">
          <Box sx={{ width: '100%', minWidth: 120 }}>
            {(() => {
              // Use consistent progress display logic
              const dispatchedPercent = job.dispatched_percent || 0;
              const searchedPercent = job.searched_percent || 0;
              const overallProgress = job.overall_progress_percent || searchedPercent;
              
              // For keyspace/rule-based jobs with multiplication factor
              if (job.effective_keyspace) {
                const progress = calculateJobProgress(job);
                return (
                  <>
                    <Box sx={{ display: 'flex', alignItems: 'center', mb: 0.5 }}>
                      <Box sx={{ width: '100%', mr: 1 }}>
                        <LinearProgress 
                          variant="determinate" 
                          value={overallProgress} 
                          sx={{ height: 6 }}
                        />
                      </Box>
                      <Box sx={{ minWidth: 45 }}>
                        <Typography variant="body2" color="text.secondary">
                          {overallProgress.toFixed(1)}%
                        </Typography>
                      </Box>
                    </Box>
                    <Typography variant="caption" color="text.secondary" display="block">
                      {searchedPercent.toFixed(3)}% / {dispatchedPercent.toFixed(3)}%
                    </Typography>
                    <Typography variant="caption" color="text.secondary">
                      {progress.displayText}
                    </Typography>
                  </>
                );
              } else {
                // Fallback for jobs without keyspace info
                return (
                  <>
                    <Box sx={{ display: 'flex', alignItems: 'center', mb: 0.5 }}>
                      <Box sx={{ width: '100%', mr: 1 }}>
                        <LinearProgress 
                          variant="determinate" 
                          value={overallProgress} 
                          sx={{ height: 6 }}
                        />
                      </Box>
                      <Box sx={{ minWidth: 45 }}>
                        <Typography variant="body2" color="text.secondary">
                          {overallProgress.toFixed(1)}%
                        </Typography>
                      </Box>
                    </Box>
                    <Typography variant="caption" color="text.secondary">
                      {searchedPercent.toFixed(3)}% / {dispatchedPercent.toFixed(3)}%
                    </Typography>
                  </>
                );
              }
            })()}
          </Box>
        </TableCell>

        {/* Keyspace */}
        <TableCell align="center">
          {job.effective_keyspace ? (
            <Tooltip title={getKeyspaceTooltip(job) || ''} arrow>
              <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 0.5 }}>
                <Typography variant="body2">
                  {formatKeyspace(job.effective_keyspace)}
                </Typography>
                {job.multiplication_factor && job.multiplication_factor > 1 && (
                  <Chip
                    label={`×${job.multiplication_factor}${job.uses_rule_splitting ? ' (rules)' : ''}`}
                    size="small"
                    color="error"
                    variant="filled"
                    icon={<InfoIcon fontSize="small" />}
                  />
                )}
              </Box>
            </Tooltip>
          ) : (
            <Typography variant="body2">-</Typography>
          )}
        </TableCell>

        {/* Cracked Count */}
        <TableCell align="center">
          {job.cracked_count > 0 ? (
            <Typography
              variant="body2"
              sx={{
                fontWeight: 'medium',
                cursor: 'pointer',
                color: 'primary.main',
                '&:hover': {
                  textDecoration: 'underline',
                },
              }}
              onClick={(e) => {
                e.stopPropagation();
                navigate(`/pot/job/${job.id}`);
              }}
            >
              {job.cracked_count.toLocaleString()}
            </Typography>
          ) : (
            <Typography variant="body2" sx={{ fontWeight: 'medium' }}>
              {job.cracked_count.toLocaleString()}
            </Typography>
          )}
        </TableCell>

        {/* Agents */}
        <TableCell align="center">
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 0.5 }}>
            <PeopleIcon fontSize="small" color="action" />
            <Typography variant="body2">
              {job.agent_count}
            </Typography>
            {job.total_speed > 0 && (
              <Tooltip title={t('tooltips.combinedHashRate')}>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, ml: 1 }}>
                  <SpeedIcon fontSize="small" color="action" />
                  <Typography variant="body2" color="text.secondary">
                    {formatters.formatHashRate(job.total_speed)}
                  </Typography>
                </Box>
              </Tooltip>
            )}
          </Box>
        </TableCell>

        {/* Priority */}
        <TableCell align="center">
          {(job.status === 'completed' || job.status === 'cancelled') ? (
            <Typography variant="body2">{job.priority}</Typography>
          ) : (
            <EditableCell
              value={job.priority}
              onSave={handleUpdatePriority}
              type="number"
              min={1}
              max={maxPriority}
              validation={(value) => {
                const num = Number(value);
                if (isNaN(num) || num < 1 || num > maxPriority) {
                  return t('validation.priorityRange', { max: maxPriority }) as string;
                }
                return null;
              }}
            />
          )}
        </TableCell>

        {/* Max Agents */}
        <TableCell align="center">
          {(job.status === 'completed' || job.status === 'cancelled') ? (
            <Typography variant="body2">{job.max_agents}</Typography>
          ) : (
            <EditableCell
              value={job.max_agents}
              onSave={handleUpdateMaxAgents}
              type="number"
              min={1}
              max={100}
              validation={(value) => {
                const num = Number(value);
                if (isNaN(num) || num < 1 || num > 100) {
                  return t('validation.maxAgentsRange') as string;
                }
                return null;
              }}
            />
          )}
        </TableCell>

        {/* Actions */}
        <TableCell align="center">
          <Box sx={{ display: 'flex', gap: 0.5, justifyContent: 'center' }}>
            {canRetry && (
              <Tooltip title={t('tooltips.retry')}>
                <IconButton
                  size="small"
                  color="primary"
                  onClick={handleRetryJob}
                  disabled={isRetrying}
                >
                  <RefreshIcon />
                </IconButton>
              </Tooltip>
            )}
            <Tooltip title={t('tooltips.delete')}>
              <IconButton
                size="small"
                color="error"
                onClick={() => setDeleteDialogOpen(true)}
              >
                <DeleteIcon />
              </IconButton>
            </Tooltip>
          </Box>
        </TableCell>
      </TableRow>

      {/* Error message row */}
      {hasError && (
        <TableRow>
          <TableCell colSpan={10} sx={{ py: 0 }}>
            <Collapse in={showError} timeout="auto" unmountOnExit>
              <Alert severity="error" sx={{ m: 2 }}>
                <Typography variant="body2">
                  <strong>{t('row.error')}</strong> {job.error_message}
                </Typography>
              </Alert>
            </Collapse>
          </TableCell>
        </TableRow>
      )}

      {/* Delete Confirmation Dialog */}
      <DeleteConfirm
        open={deleteDialogOpen}
        onClose={() => setDeleteDialogOpen(false)}
        onConfirm={handleDeleteJob}
        isLoading={isDeleting}
        title={t('confirmDelete.title') as string}
        message={t('confirmDelete.messageNamed', { name: job.name }) as string}
      />
    </>
  );
};

export default JobRow;