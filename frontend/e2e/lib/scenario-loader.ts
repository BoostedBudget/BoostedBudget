/**
 * Scenario Loader
 *
 * Loads and validates YAML scenario files
 * Handles template loading and anchor resolution
 * Provides detailed error messages for invalid scenarios
 *
 * Issue #331: E2E Test Scenarios Framework
 */

import fs from 'fs';
import path from 'path';
import yaml from 'js-yaml';
import { ScenarioSchema, type Scenario } from './types';
import { ZodError } from 'zod';

/**
 * Scenario loader class
 * Handles YAML parsing and validation
 */
export class ScenarioLoader {
  private scenariosDir: string;
  private templatesDir: string;

  /**
   * Create scenario loader
   * @param scenariosDir Path to scenarios directory
   * @param templatesDir Path to templates directory
   */
  constructor(
    scenariosDir: string = path.join(process.cwd(), 'e2e/scenarios'),
    templatesDir: string = path.join(process.cwd(), 'e2e/templates')
  ) {
    this.scenariosDir = scenariosDir;
    this.templatesDir = templatesDir;
  }

  /**
   * Load a scenario from YAML file
   * @param scenarioPath Path to scenario file (relative or absolute)
   * @returns Parsed and validated scenario
   */
  async load(scenarioPath: string): Promise<Scenario> {
    try {
      // Resolve path (handle both relative and absolute)
      const fullPath = path.isAbsolute(scenarioPath)
        ? scenarioPath
        : path.join(this.scenariosDir, scenarioPath);

      // Check if file exists
      if (!fs.existsSync(fullPath)) {
        throw new Error(`Scenario file not found: ${fullPath}`);
      }

      // Read YAML file
      const content = fs.readFileSync(fullPath, 'utf8');

      // Parse YAML (js-yaml handles anchors/aliases automatically)
      const rawScenario = yaml.load(content);

      if (!rawScenario || typeof rawScenario !== 'object') {
        throw new Error(`Invalid YAML content in ${fullPath}`);
      }

      // Validate with Zod schema
      const scenario = ScenarioSchema.parse(rawScenario);

      return scenario;
    } catch (error) {
      if (error instanceof ZodError) {
        throw this.formatZodError(error, scenarioPath);
      }
      throw error;
    }
  }

  /**
   * Load multiple scenarios from a directory
   * @param pattern Glob pattern or directory path
   * @returns Array of loaded scenarios with their paths
   */
  async loadMultiple(
    pattern: string
  ): Promise<Array<{ path: string; scenario: Scenario }>> {
    const scenarioFiles = this.findScenarioFiles(pattern);
    const results: Array<{ path: string; scenario: Scenario }> = [];

    for (const filePath of scenarioFiles) {
      try {
        const scenario = await this.load(filePath);
        results.push({ path: filePath, scenario });
      } catch (error) {
        console.error(`Failed to load scenario ${filePath}:`, error);
        throw error;
      }
    }

    return results;
  }

  /**
   * Load a template from YAML file
   * @param templateName Name of template file (without .yaml extension)
   * @returns Parsed template data
   */
  async loadTemplate(templateName: string): Promise<any> {
    const templatePath = path.join(this.templatesDir, `${templateName}.yaml`);

    if (!fs.existsSync(templatePath)) {
      throw new Error(`Template not found: ${templatePath}`);
    }

    const content = fs.readFileSync(templatePath, 'utf8');
    const template = yaml.load(content);

    return template;
  }

  /**
   * Validate a scenario without loading from file
   * Useful for testing
   */
  validate(rawScenario: any): Scenario {
    try {
      return ScenarioSchema.parse(rawScenario);
    } catch (error) {
      if (error instanceof ZodError) {
        throw this.formatZodError(error, '<inline>');
      }
      throw error;
    }
  }

  /**
   * Find scenario files matching a pattern
   */
  private findScenarioFiles(pattern: string): string[] {
    const fullPattern = path.isAbsolute(pattern)
      ? pattern
      : path.join(this.scenariosDir, pattern);

    // If pattern is a directory, find all .yaml files recursively
    if (fs.existsSync(fullPattern) && fs.statSync(fullPattern).isDirectory()) {
      return this.findYamlFiles(fullPattern);
    }

    // If pattern is a file, return it
    if (fs.existsSync(fullPattern) && fs.statSync(fullPattern).isFile()) {
      return [fullPattern];
    }

    // TODO: Implement glob pattern matching if needed
    throw new Error(`Pattern not supported yet: ${pattern}`);
  }

  /**
   * Recursively find all .yaml files in a directory
   */
  private findYamlFiles(dir: string): string[] {
    const files: string[] = [];

    const entries = fs.readdirSync(dir, { withFileTypes: true });

    for (const entry of entries) {
      const fullPath = path.join(dir, entry.name);

      if (entry.isDirectory()) {
        files.push(...this.findYamlFiles(fullPath));
      } else if (entry.isFile() && entry.name.endsWith('.yaml')) {
        files.push(fullPath);
      }
    }

    return files;
  }

  /**
   * Format Zod validation error with detailed context
   */
  private formatZodError(error: ZodError, scenarioPath: string): Error {
    const messages: string[] = [
      `\n❌ Scenario validation failed: ${scenarioPath}\n`,
    ];

    for (const issue of error.issues) {
      const path = issue.path.join('.');
      const message = issue.message;
      const code = issue.code;

      messages.push(`  Path: ${path || '(root)'}`);
      messages.push(`  Error: ${message}`);
      messages.push(`  Code: ${code}`);

      if ('expected' in issue) {
        messages.push(`  Expected: ${issue.expected}`);
      }
      if ('received' in issue) {
        messages.push(`  Received: ${issue.received}`);
      }

      messages.push(''); // Empty line between issues
    }

    messages.push('📖 See scenarios/schema.yaml for complete schema documentation\n');

    return new Error(messages.join('\n'));
  }

  /**
   * Get scenarios directory path
   */
  getScenariosDir(): string {
    return this.scenariosDir;
  }

  /**
   * Get templates directory path
   */
  getTemplatesDir(): string {
    return this.templatesDir;
  }

  /**
   * Check if a scenario file exists
   */
  exists(scenarioPath: string): boolean {
    const fullPath = path.isAbsolute(scenarioPath)
      ? scenarioPath
      : path.join(this.scenariosDir, scenarioPath);

    return fs.existsSync(fullPath);
  }
}
