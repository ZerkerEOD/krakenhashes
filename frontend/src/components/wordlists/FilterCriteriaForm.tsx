import React, { useEffect, useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Grid,
  TextField,
  FormControlLabel,
  Checkbox,
  Typography,
  Chip,
  CircularProgress,
  Tooltip,
} from '@mui/material';
import { WordlistFilter } from '../../types/wordlists';
import { previewFilter } from '../../services/wordlists';

interface FilterCriteriaFormProps {
  /** Parent wordlist ID used for the live count preview (optional). */
  parentWordlistId?: number;
  value: WordlistFilter;
  onChange: (filter: WordlistFilter) => void;
  /** Whether to show the live candidate-count preview (needs a numeric parent). */
  showPreview?: boolean;
}

// isFilterEmpty mirrors the backend's IsEmpty so we can disable preview/submit.
export const isFilterEmpty = (f: WordlistFilter): boolean => {
  return (
    (f.min_length === undefined || f.min_length === null) &&
    (f.max_length === undefined || f.max_length === null) &&
    !f.require_upper &&
    !f.require_lower &&
    !f.require_digit &&
    !f.require_special &&
    (f.min_classes === undefined || f.min_classes === null) &&
    !f.regex
  );
};

const FilterCriteriaForm: React.FC<FilterCriteriaFormProps> = ({
  parentWordlistId,
  value,
  onChange,
  showPreview = true,
}) => {
  const { t } = useTranslation('admin');
  const [preview, setPreview] = useState<{ count: number; rate: number } | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [regexError, setRegexError] = useState<string | null>(null);

  const update = (patch: Partial<WordlistFilter>) => {
    onChange({ ...value, ...patch });
  };

  const parseNum = (s: string): number | null => {
    if (s === '') return null;
    const n = parseInt(s, 10);
    return isNaN(n) ? null : n;
  };

  // Validate regex client-side for immediate feedback. The backend uses Go RE2,
  // which (unlike JS RegExp) does NOT support lookaround — so we flag those here
  // to avoid a confusing accept-then-reject on preview/create.
  useEffect(() => {
    if (!value.regex) {
      setRegexError(null);
      return;
    }
    if (/\(\?(=|!|<=|<!)/.test(value.regex)) {
      setRegexError(t('wordlists.filterForm.regexLookaroundError') as string);
      return;
    }
    try {
      // eslint-disable-next-line no-new
      new RegExp(value.regex);
      setRegexError(null);
    } catch (e: any) {
      setRegexError(e.message || (t('wordlists.filterForm.regexInvalidDefault') as string));
    }
  }, [value.regex, t]);

  // Debounced live preview of the resulting candidate count.
  const runPreview = useCallback(() => {
    if (!showPreview || !parentWordlistId || isFilterEmpty(value) || regexError) {
      setPreview(null);
      return;
    }
    let cancelled = false;
    setPreviewing(true);
    previewFilter(parentWordlistId, value)
      .then((resp) => {
        if (!cancelled) {
          setPreview({ count: resp.data.estimated_count, rate: resp.data.match_rate });
        }
      })
      .catch(() => {
        if (!cancelled) setPreview(null);
      })
      .finally(() => {
        if (!cancelled) setPreviewing(false);
      });
    return () => {
      cancelled = true;
    };
  }, [showPreview, parentWordlistId, value, regexError]);

  useEffect(() => {
    const timer = setTimeout(runPreview, 600);
    return () => clearTimeout(timer);
  }, [runPreview]);

  return (
    <Box>
      <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1.5 }}>
        {t('wordlists.filterForm.helpText')}
      </Typography>
      <Grid container spacing={2}>
        <Grid item xs={6}>
          <TextField
            label={t('wordlists.filterForm.minLength')}
            type="number"
            size="small"
            fullWidth
            value={value.min_length ?? ''}
            onChange={(e) => update({ min_length: parseNum(e.target.value) })}
            inputProps={{ min: 0 }}
          />
        </Grid>
        <Grid item xs={6}>
          <TextField
            label={t('wordlists.filterForm.maxLength')}
            type="number"
            size="small"
            fullWidth
            value={value.max_length ?? ''}
            onChange={(e) => update({ max_length: parseNum(e.target.value) })}
            inputProps={{ min: 0 }}
          />
        </Grid>
      </Grid>

      <Typography variant="subtitle2" sx={{ mt: 2, mb: 0.5 }}>
        {t('wordlists.filterForm.requiredClasses')}
      </Typography>
      <Box sx={{ display: 'flex', flexWrap: 'wrap' }}>
        <FormControlLabel
          control={<Checkbox checked={!!value.require_upper} onChange={(e) => update({ require_upper: e.target.checked })} />}
          label={t('wordlists.filterForm.uppercase') as string}
        />
        <FormControlLabel
          control={<Checkbox checked={!!value.require_lower} onChange={(e) => update({ require_lower: e.target.checked })} />}
          label={t('wordlists.filterForm.lowercase') as string}
        />
        <FormControlLabel
          control={<Checkbox checked={!!value.require_digit} onChange={(e) => update({ require_digit: e.target.checked })} />}
          label={t('wordlists.filterForm.digit') as string}
        />
        <FormControlLabel
          control={<Checkbox checked={!!value.require_special} onChange={(e) => update({ require_special: e.target.checked })} />}
          label={t('wordlists.filterForm.special') as string}
        />
      </Box>

      <Grid container spacing={2} sx={{ mt: 0 }}>
        <Grid item xs={6}>
          <TextField
            label={t('wordlists.filterForm.minClasses')}
            type="number"
            size="small"
            fullWidth
            value={value.min_classes ?? ''}
            onChange={(e) => {
              const n = parseNum(e.target.value);
              update({ min_classes: n === null ? null : Math.max(1, Math.min(4, n)) });
            }}
            inputProps={{ min: 1, max: 4 }}
            helperText={t('wordlists.filterForm.minClassesHelp') as string}
          />
        </Grid>
        <Grid item xs={6}>
          <TextField
            label={t('wordlists.filterForm.regexLabel')}
            size="small"
            fullWidth
            value={value.regex ?? ''}
            onChange={(e) => update({ regex: e.target.value })}
            error={!!regexError}
            helperText={regexError || (t('wordlists.filterForm.regexHelperDefault') as string)}
          />
        </Grid>
      </Grid>

      {showPreview && parentWordlistId && !isFilterEmpty(value) && (
        <Box sx={{ mt: 2, display: 'flex', alignItems: 'center', gap: 1 }}>
          {previewing ? (
            <>
              <CircularProgress size={16} />
              <Typography variant="body2" color="text.secondary">
                {t('wordlists.filterForm.estimating')}
              </Typography>
            </>
          ) : preview ? (
            <Tooltip title={t('wordlists.filterForm.previewTooltip') as string}>
              <Chip
                size="small"
                color="primary"
                variant="outlined"
                label={t('wordlists.filterForm.previewLabel', {
                  countDisplay: preview.count.toLocaleString(),
                  rateDisplay: (preview.rate * 100).toFixed(1),
                }) as string}
              />
            </Tooltip>
          ) : (
            <Typography variant="body2" color="text.secondary">
              {t('wordlists.filterForm.previewUnavailable')}
            </Typography>
          )}
        </Box>
      )}
    </Box>
  );
};

export default FilterCriteriaForm;
