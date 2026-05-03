// Stub: footer data provider
export interface ReadonlyFooterDataProvider {
  getModel(): string;
  getThinkingLevel(): string;
  getTokens(): { input: number; output: number };
  getCost(): string;
  getBranch(): string;
  getPwd(): string;
}
