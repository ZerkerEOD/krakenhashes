import React from 'react';
import { useTranslation } from 'react-i18next';
import { Box, Tooltip, Typography } from '@mui/material';
import { getStatusTone } from './ui/statusMaps';
import type { StatusTone } from '../styles/palette';

/** Theme palette path for a status tone (resolved by `sx`). */
const toneColor = (tone: StatusTone): string => (tone === 'default' ? 'action.disabled' : `${tone}.main`);
import { JobTask, JobIncrementLayer } from '../types/jobs';

interface JobProgressBarProps {
  tasks: JobTask[];
  totalKeyspace: number;
  height?: number;
  // For increment-mode jobs only. Each layer's task stores LAYER-LOCAL
  // effective_keyspace_start/end (Step 11c backend change) — the bar
  // composes them into one continuous coordinate space by applying a
  // per-layer offset of sum(prior layers' effective_keyspace). For
  // non-increment jobs leave undefined; tasks then render at their
  // stored coords without offset.
  layers?: JobIncrementLayer[];
}

interface TaskSegment {
  task: JobTask;
  startPercent: number;
  widthPercent: number;
  color: string;
  cracksFound: number;
  zIndex: number;
}

// Tiered display priority. Higher z-index renders on top — so a 'completed'
// segment naturally hides an underlying 'failed' segment from a prior
// rejected/recovered attempt at the same keyspace range. Where the lower-tier
// segment extends beyond the higher-tier one (a partial failure not yet
// re-attempted), the bare portion of the lower-tier still shows.
const STATUS_TIER: Record<string, number> = {
  completed: 5,
  processing: 4,
  running: 3,
  processing_error: 2,
  failed: 1,
  pending: 0,
};

const JobProgressBar: React.FC<JobProgressBarProps> = ({
  tasks,
  totalKeyspace,
  height = 40,
  layers,
}) => {
  const { t } = useTranslation('jobs');
  // For increment-mode jobs, precompute the per-layer effective-keyspace
  // offset: layer N's display coords = layerOffset[N] + task.effective_start.
  // For non-increment jobs (layers undefined/empty) every task gets offset=0.
  const layerOffsetById: Record<string, number> = {};
  if (layers && layers.length > 0) {
    const sortedLayers = [...layers].sort((a, b) => a.layer_index - b.layer_index);
    let cumulative = 0;
    for (const layer of sortedLayers) {
      layerOffsetById[layer.id] = cumulative;
      // effective_keyspace is a decimal string (NUMERIC) — coerce before summing.
      cumulative += Number(layer.effective_keyspace ?? 0);
    }
  }

  // Calculate segments for visualization
  const segments: TaskSegment[] = tasks.map(task => {
    // Use effective keyspace if available, otherwise use regular keyspace.
    // effective_keyspace_start/end are decimal strings (NUMERIC) — coerce to Number.
    const rawStart = Number(task.effective_keyspace_start ?? task.keyspace_start);
    const rawEnd = Number(task.effective_keyspace_end ?? task.keyspace_end);

    const offset = task.increment_layer_id ? (layerOffsetById[task.increment_layer_id] ?? 0) : 0;
    const start = rawStart + offset;
    const end = rawEnd + offset;

    const startPercent = (start / totalKeyspace) * 100;
    const widthPercent = ((end - start) / totalKeyspace) * 100;

    // Colour from the canonical task status map (theme-aware).
    const color = toneColor(getStatusTone('task', task.status));

    return {
      task,
      startPercent,
      widthPercent,
      color,
      cracksFound: task.crack_count || 0,
      zIndex: STATUS_TIER[task.status] ?? 0,
    };
  });

  // Calculate overall progress. Failed tasks are excluded — their interval's
  // range reopens as a gap and gets re-dispatched, so counting their
  // effective_keyspace_processed would double-count work alongside the
  // re-attempt.
  const totalProcessed = tasks.reduce((sum, task) => {
    if (task.status === 'failed' || task.status === 'cancelled') {
      return sum;
    }
    const processed = Number(task.effective_keyspace_processed ?? task.keyspace_processed);
    return sum + processed;
  }, 0);
  // Cap at 100% to prevent display issues when effective_keyspace from benchmark was lower than actual
  const rawProgress = totalKeyspace > 0 ? (totalProcessed / totalKeyspace) * 100 : 0;
  const overallProgress = Math.min(rawProgress, 100);

  const formatKeyspace = (value: number | string): string => {
    const v = Number(value);
    if (!isFinite(v)) return '0';
    if (v >= 1e12) return `${(v / 1e12).toFixed(2)}T`;
    if (v >= 1e9) return `${(v / 1e9).toFixed(2)}B`;
    if (v >= 1e6) return `${(v / 1e6).toFixed(2)}M`;
    if (v >= 1e3) return `${(v / 1e3).toFixed(2)}K`;
    return v.toString();
  };

  const formatSpeed = (speed?: number): string => {
    if (!speed) return t('progressBar.notAvailable') as string;
    if (speed >= 1e12) return `${(speed / 1e12).toFixed(2)} TH/s`;
    if (speed >= 1e9) return `${(speed / 1e9).toFixed(2)} GH/s`;
    if (speed >= 1e6) return `${(speed / 1e6).toFixed(2)} MH/s`;
    if (speed >= 1e3) return `${(speed / 1e3).toFixed(2)} KH/s`;
    return `${speed} H/s`;
  };

  return (
    <Box sx={{ width: '100%' }}>
      {/* Progress percentage */}
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
        {t('progressBar.overallProgress', { percent: overallProgress.toFixed(2) })}
      </Typography>
      
      {/* Progress bar container */}
      <Box 
        sx={{ 
          position: 'relative',
          width: '100%',
          height: height,
          backgroundColor: 'surface.sunken',
          borderRadius: 1,
          overflow: 'hidden',
          border: '1px solid',
          borderColor: 'divider',
        }}
      >
        {/* Render segments */}
        {segments.map((segment, index) => (
          <Tooltip
            key={segment.task.id}
            title={
              <Box>
                <Typography variant="body2">{t('progressBar.tooltip.taskId', { id: segment.task.id.slice(0, 8) })}</Typography>
                <Typography variant="body2">{t('progressBar.tooltip.status', { status: segment.task.status })}</Typography>
                <Typography variant="body2">
                  {t('progressBar.tooltip.keyspace', {
                    start: formatKeyspace(segment.task.effective_keyspace_start ?? segment.task.keyspace_start),
                    end: formatKeyspace(segment.task.effective_keyspace_end ?? segment.task.keyspace_end),
                  })}
                </Typography>
                <Typography variant="body2">
                  {t('progressBar.tooltip.progress', {
                    percent: (() => {
                      const p = segment.task.progress_percent ?? 0;
                      const shown = segment.task.status === 'running' ? Math.min(p, 99.99) : p;
                      return shown.toFixed(2);
                    })(),
                  })}
                </Typography>
                {segment.task.benchmark_speed && (
                  <Typography variant="body2">
                    {t('progressBar.tooltip.speed', { speed: formatSpeed(segment.task.benchmark_speed) })}
                  </Typography>
                )}
                {segment.cracksFound > 0 && (
                  <Typography variant="body2">
                    {t('progressBar.tooltip.cracksFound', { count: segment.cracksFound })}
                  </Typography>
                )}
                {segment.task.agent_id && (
                  <Typography variant="body2">
                    {t('progressBar.tooltip.agentId', { id: segment.task.agent_id })}
                  </Typography>
                )}
              </Box>
            }
            arrow
            placement="top"
          >
            <Box
              sx={{
                position: 'absolute',
                left: `${segment.startPercent}%`,
                width: `${segment.widthPercent}%`,
                height: '100%',
                backgroundColor: segment.color,
                // Tiered z-index: completed/running/processing render OVER
                // failed segments at the same range, so a re-attempted chunk
                // hides the failed predecessor. Where the failed range
                // extends beyond the re-attempt, the bare portion shows.
                zIndex: segment.zIndex,
                transition: 'all 0.3s ease',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                cursor: 'pointer',
                '&:hover': {
                  opacity: 0.8
                }
              }}
            >
              {/* Show progress within running tasks */}
              {segment.task.status === 'running' && segment.task.progress_percent && (
                <Box
                  sx={{
                    position: 'absolute',
                    left: 0,
                    width: `${Math.min(segment.task.progress_percent ?? 0, 99.99)}%`,
                    height: '100%',
                    backgroundColor: 'common.white',
                    opacity: 0.3,
                    transition: 'width 0.3s ease'
                  }}
                />
              )}
              
              {/* Render crack indicators as red vertical lines */}
              {segment.cracksFound > 0 && (
                <>
                  {Array.from({ length: Math.min(segment.cracksFound, 10) }).map((_, crackIndex) => {
                    // Distribute cracks evenly across the segment
                    const crackPosition = ((crackIndex + 1) / (segment.cracksFound + 1)) * 100;
                    return (
                      <Box
                        key={`crack-${segment.task.id}-${crackIndex}`}
                        sx={{
                          position: 'absolute',
                          left: `${crackPosition}%`,
                          width: '2px',
                          height: '100%',
                          backgroundColor: 'error.dark',
                          zIndex: 2
                        }}
                      />
                    );
                  })}
                </>
              )}
            </Box>
          </Tooltip>
        ))}
        
        {/* If no tasks or keyspace is 0, show empty state */}
        {(segments.length === 0 || totalKeyspace === 0) && (
          <Box
            sx={{
              position: 'absolute',
              width: '100%',
              height: '100%',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center'
            }}
          >
            <Typography variant="body2" color="text.secondary">
              {t('progressBar.noTasks')}
            </Typography>
          </Box>
        )}
      </Box>
      
      {/* Legend */}
      <Box sx={{ display: 'flex', gap: 2, mt: 1, flexWrap: 'wrap' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
          <Box sx={{ width: 16, height: 16, backgroundColor: toneColor('queued'), borderRadius: 0.5 }} />
          <Typography variant="caption">{t('progressBar.legend.pending')}</Typography>
        </Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
          <Box sx={{ width: 16, height: 16, backgroundColor: toneColor('running'), borderRadius: 0.5 }} />
          <Typography variant="caption">{t('progressBar.legend.running')}</Typography>
        </Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
          <Box sx={{ width: 16, height: 16, backgroundColor: toneColor('info'), borderRadius: 0.5 }} />
          <Typography variant="caption">{t('progressBar.legend.processing')}</Typography>
        </Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
          <Box sx={{ width: 16, height: 16, backgroundColor: toneColor('success'), borderRadius: 0.5 }} />
          <Typography variant="caption">{t('progressBar.legend.completed')}</Typography>
        </Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
          <Box sx={{ width: 16, height: 16, backgroundColor: toneColor('warning'), borderRadius: 0.5 }} />
          <Typography variant="caption">{t('progressBar.legend.processingError')}</Typography>
        </Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
          <Box sx={{ width: 16, height: 16, backgroundColor: toneColor('error'), borderRadius: 0.5 }} />
          <Typography variant="caption">{t('progressBar.legend.failed')}</Typography>
        </Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
          <Box sx={{ width: 2, height: 16, backgroundColor: 'error.dark' }} />
          <Typography variant="caption">{t('progressBar.legend.crackFound')}</Typography>
        </Box>
      </Box>
    </Box>
  );
};

export default JobProgressBar;