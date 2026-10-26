export type Numeric = number | { toString(): string };
export type Literal = { number?: Numeric; boolean?: boolean; string?: string };
export type Condition = {
  op: string;
  parameter?: string;
  other_parameter?: string;
  value?: Literal;
  values?: Literal[];
  conditions?: Condition[];
};
export type Source = { kind: string; path: string };
export type Parameter = {
  id: string;
  title: string;
  enabled: boolean;
  source: Source;
  type: string;
  unit: string;
  required: boolean;
  depends_on?: string[];
  number?: {
    integer: boolean;
    lower?: { value: Numeric; inclusive: boolean };
    upper?: { value: Numeric; inclusive: boolean };
  };
};
export type ScoreCase = {
  id: string;
  when: Condition;
  points: Numeric;
  description?: string;
  review_reason?: { message: string };
};
export type Criterion = {
  id: string;
  title: string;
  description?: string;
  enabled: boolean;
  block: string;
  max_points: Numeric;
  value_parameter?: string;
  applies_when?: Condition;
  unknown_action: string;
  cases: ScoreCase[];
  special_cases: ScoreCase[];
  answer?: unknown;
};
export type Rule = {
  id: string;
  title: string;
  enabled: boolean;
  when?: Condition;
  satisfied_when?: Condition;
  applies_when?: Condition;
  reason?: { code: string; message: string; fields: string[] };
  message?: string;
  requires_checks?: string[];
  after_criterion?: string;
  [key: string]: unknown;
};
export type Definition = {
  format_version: Numeric;
  id: string;
  version: string;
  borrower_type: string;
  title: string;
  description?: string;
  parameters: Parameter[];
  criteria: Criterion[];
  data_checks: Rule[];
  stops: Rule[];
  manual_reviews: Rule[];
  classes: { grade: string; min_total: Numeric; max_total: Numeric }[];
};
export type Draft = {
  id: string;
  revision: Numeric;
  definition: Definition;
  definition_sha256: string;
  created_by: string;
  created_at: string;
  updated_at: string;
};
export type Published = {
  version: string;
  definition: Definition;
  definition_sha256: string;
  created_by: string;
  created_at: string;
};
export type Active = {
  borrower_type: string;
  version: string;
  revision: Numeric;
  definition_sha256: string;
  changed_by: string;
  changed_at: string;
};
export type Validation = {
  revision: Numeric;
  validation: {
    valid: boolean;
    issues: { path: string; code: string; message: string }[];
  };
};
export type Revision = {
  revision: Numeric;
  author: string;
  reason: string;
  created_at: string;
};
export type Catalog = {
  systems: { id: string; title: string; collection_condition: string }[];
  fields: {
    id: string;
    system: string;
    path: string;
    title: string;
    type: string;
    bindings: Source[];
    used: boolean;
    derived: boolean;
    roles: string[];
    criteria: string[];
    reason: string;
  }[];
};
