// Analytics types for password analysis

export interface AnalyticsReport {
  id: string;
  client_id: string;
  user_id: string;
  start_date: string;
  end_date: string;
  status: 'queued' | 'processing' | 'completed' | 'failed';
  analytics_data?: AnalyticsData;
  total_hashlists: number;
  total_hashes: number;
  total_cracked: number;
  queue_position?: number;
  custom_patterns: string[];
  created_at: string;
  started_at?: string;
  completed_at?: string;
  error_message?: string;
}

export interface AnalyticsData {
  overview: OverviewStats;
  windows_hashes?: WindowsHashStats;
  length_distribution: LengthStats;
  complexity_analysis: ComplexityStats;
  positional_analysis: PositionalStats;
  pattern_detection: PatternStats;
  username_correlation: UsernameStats;
  password_reuse: ReuseStats;
  hash_reuse?: HashReuseStats;
  temporal_patterns: TemporalStats;
  mask_analysis: MaskStats;
  custom_patterns: CustomPatternStats;
  strength_metrics: StrengthStats;
  top_passwords: TopPassword[];
  lm_partial_cracks?: LMPartialCrackStats;
  lm_to_ntlm_masks?: LMToNTLMMaskStats;
  recommendations: Recommendation[];
  domain_analytics?: DomainAnalytics[];
}

export interface DomainAnalytics {
  domain: string;
  overview: OverviewStats;
  windows_hashes?: WindowsHashStats;
  length_distribution: LengthStats;
  complexity_analysis: ComplexityStats;
  positional_analysis: PositionalStats;
  pattern_detection: PatternStats;
  username_correlation: UsernameStats;
  password_reuse: ReuseStats;
  hash_reuse?: HashReuseStats;
  temporal_patterns: TemporalStats;
  mask_analysis: MaskStats;
  custom_patterns: CustomPatternStats;
  strength_metrics: StrengthStats;
  top_passwords: TopPassword[];
  lm_partial_cracks?: LMPartialCrackStats;
  lm_to_ntlm_masks?: LMToNTLMMaskStats;
}

export interface OverviewStats {
  total_hashes: number;
  total_cracked: number;
  crack_percentage: number;
  hash_modes: HashModeStats[];
  domain_breakdown: DomainStats[];
}

export interface HashModeStats {
  mode_id: number;
  mode_name: string;
  total: number;
  cracked: number;
  percentage: number;
}

export interface DomainStats {
  domain: string;
  total_hashes: number;
  cracked_hashes: number;
  crack_percentage: number;
}

export interface LengthStats {
  distribution: Record<string, CategoryCount>;
  average_length: number;
  average_length_under_15: number;
  most_common_lengths: number[];
  count_under_8: number;
  count_8_to_11: number;
  count_under_15: number;
}

export interface ComplexityStats {
  single_type: Record<string, CategoryCount>;
  two_types: Record<string, CategoryCount>;
  three_types: Record<string, CategoryCount>;
  four_types: CategoryCount;
  complex_short: CategoryCount;
  complex_long: CategoryCount;
}

export interface CategoryCount {
  count: number;
  percentage: number;
}

export interface PositionalStats {
  starts_uppercase: CategoryCount;
  ends_number: CategoryCount;
  ends_special: CategoryCount;
}

export interface PatternStats {
  keyboard_walks: CategoryCount;
  sequential: CategoryCount;
  repeating_chars: CategoryCount;
  common_base_words: Record<string, CategoryCount>;
}

export interface UsernameStats {
  equals_username: CategoryCount;
  contains_username: CategoryCount;
  username_plus_suffix: CategoryCount;
  reversed_username: CategoryCount;
}

export interface ReuseStats {
  total_reused: number;
  percentage_reused: number;
  total_unique: number;
  password_reuse_info: PasswordReuseInfo[];
}

export interface PasswordReuseInfo {
  password: string;
  users: UserOccurrence[];
  total_occurrences: number;
  user_count: number;
}

export interface UserOccurrence {
  username: string;
  hashlist_count: number;
}

export interface TemporalStats {
  contains_year: CategoryCount;
  contains_month: CategoryCount;
  contains_season: CategoryCount;
  year_breakdown: Record<string, CategoryCount>;
}

export interface MaskStats {
  top_masks: MaskInfo[];
}

export interface MaskInfo {
  mask: string;
  count: number;
  percentage: number;
  example: string;
}

export interface CustomPatternStats {
  patterns_detected: Record<string, CategoryCount>;
}

export interface StrengthStats {
  average_speed_hps: number;
  entropy_distribution: EntropyDistribution;
  crack_time_estimates: CrackTimeEstimates;
}

export interface EntropyDistribution {
  low: CategoryCount;
  moderate: CategoryCount;
  high: CategoryCount;
}

export interface CrackTimeEstimates {
  speed_50_percent: SpeedLevelEstimate;
  speed_75_percent: SpeedLevelEstimate;
  speed_100_percent: SpeedLevelEstimate;
  speed_150_percent: SpeedLevelEstimate;
  speed_200_percent: SpeedLevelEstimate;
}

export interface SpeedLevelEstimate {
  speed_hps: number;
  percent_under_1_hour: number;
  percent_under_1_day: number;
  percent_under_1_week: number;
  percent_under_1_month: number;
  percent_under_6_months: number;
  percent_under_1_year: number;
  percent_over_1_year: number;
}

export interface TopPassword {
  password: string;
  count: number;
  percentage: number;
}

export interface Recommendation {
  severity: 'CRITICAL' | 'HIGH' | 'MEDIUM' | 'INFO';
  count: number;
  percentage: number;
  message: string;
}

export interface CreateAnalyticsReportRequest {
  client_id: string;
  start_date: string;
  end_date: string;
  custom_patterns?: string[];
}

export interface QueueStatus {
  queue_length: number;
  is_processing: boolean;
}

// Windows Hash Analytics Types
export interface WindowsHashStats {
  overview: WindowsOverviewStats;
  ntlm?: WindowsHashTypeStats;
  lm?: LMHashStats;
  netntlmv1?: WindowsHashTypeStats;
  netntlmv2?: WindowsHashTypeStats;
  dcc?: WindowsHashTypeStats;
  dcc2?: WindowsHashTypeStats;
  kerberos?: KerberosStats;
  linkedCorrelation?: LinkedHashCorrelationStats;
}

export interface WindowsOverviewStats {
  total_windows: number;
  cracked_windows: number;
  percentage_windows: number;
  unique_users: number;
  linked_pairs: number;
}

export interface WindowsHashTypeStats {
  total: number;
  cracked: number;
  percentage: number;
}

export interface LMHashStats extends WindowsHashTypeStats {
  under_8: number;
  '8_to_14': number;
  partially_cracked: number;
}

export interface KerberosStats extends WindowsHashTypeStats {
  by_type?: Record<string, WindowsHashTypeStats>;
}

export interface LinkedHashCorrelationStats {
  total_linked_pairs: number;
  both_cracked: number;
  percentage_both: number;
  only_ntlm_cracked: number;
  only_lm_cracked: number;
  neither_cracked: number;
}

// Hash Reuse Types
export interface HashReuseStats {
  total_reused: number;
  percentage_reused: number;
  total_unique: number;
  hash_reuse_info: HashReuseInfo[];
}

export interface HashReuseInfo {
  hash_value: string;
  hash_type: string;
  password?: string;
  users: UserOccurrence[];
  total_occurrences: number;
  user_count: number;
}

// LM Partial Crack Types
export interface LMPartialCrackStats {
  total_partial: number;
  first_half_only: number;
  second_half_only: number;
  percentage_partial: number;
  partial_crack_details: LMPartialCrackDetail[];
}

export interface LMPartialCrackDetail {
  username?: string;
  domain?: string;
  first_half_cracked: boolean;
  first_half_pwd?: string;
  second_half_cracked: boolean;
  second_half_pwd?: string;
  hashlist_name: string;
}

// LM-to-NTLM Mask Types
export interface LMToNTLMMaskStats {
  total_lm_cracked: number;
  total_masks_generated: number;
  total_estimated_keyspace: number;
  masks: LMNTLMMaskInfo[];
}

export interface LMNTLMMaskInfo {
  mask: string;
  lm_pattern: string;
  count: number;
  percentage: number;
  match_percentage: number;
  estimated_keyspace: number;
  example_lm: string;
}
