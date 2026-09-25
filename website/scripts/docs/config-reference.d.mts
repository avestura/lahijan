/** Types for config-reference.mjs (the generator is plain ESM so Node runs it directly). */
export declare const CONFIG_FILE: string;
export declare const DESCRIPTIONS_FILE: string;
export declare const OUTPUT_FILE: string;
export declare function envName(key: string): string;
export declare function renderConfigReference(configYaml: string, descriptionsYaml: string): string;
