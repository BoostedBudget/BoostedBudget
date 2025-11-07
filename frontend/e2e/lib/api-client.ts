/**
 * API Client
 *
 * HTTP request wrapper for making API calls during scenario execution
 * Handles authentication, headers, and response validation
 *
 * Issue #331: E2E Test Scenarios Framework
 */

import axios, { AxiosInstance, AxiosResponse, AxiosRequestConfig } from 'axios';

/**
 * API request configuration
 */
export interface ApiRequest {
  method: 'GET' | 'POST' | 'PUT' | 'DELETE' | 'PATCH';
  url: string;
  headers?: Record<string, string>;
  params?: Record<string, any>;
  body?: any;
  timeout?: number;
}

/**
 * API response wrapper
 */
export interface ApiResponse<T = any> {
  status: number;
  statusText: string;
  headers: Record<string, string>;
  data: T;
}

/**
 * Login credentials
 */
export interface LoginCredentials {
  email: string;
  password: string;
}

/**
 * API Client class
 * Wraps axios for consistent API interactions in scenarios
 */
export class ApiClient {
  private client: AxiosInstance;
  private token: string | null = null;
  private baseURL: string;

  /**
   * Create API client
   * @param baseURL Base URL for API (e.g., http://localhost:8081)
   */
  constructor(baseURL: string = 'http://localhost:8081') {
    this.baseURL = baseURL;
    this.client = axios.create({
      baseURL,
      timeout: 30000, // 30 second default timeout
      validateStatus: () => true, // Don't throw on any status code
    });
  }

  /**
   * Make an HTTP request
   */
  async request<T = any>(config: ApiRequest): Promise<ApiResponse<T>> {
    const axiosConfig: AxiosRequestConfig = {
      method: config.method,
      url: config.url,
      headers: {
        'Content-Type': 'application/json',
        ...config.headers,
      },
      params: config.params,
      data: config.body,
      timeout: config.timeout || 30000,
    };

    // Add authentication token if available
    if (this.token) {
      axiosConfig.headers = {
        ...axiosConfig.headers,
        Authorization: `Bearer ${this.token}`,
      };
    }

    try {
      const response: AxiosResponse<T> = await this.client.request(axiosConfig);

      return {
        status: response.status,
        statusText: response.statusText,
        headers: response.headers as Record<string, string>,
        data: response.data,
      };
    } catch (error) {
      if (axios.isAxiosError(error)) {
        if (error.response) {
          // Server responded with error status
          return {
            status: error.response.status,
            statusText: error.response.statusText,
            headers: error.response.headers as Record<string, string>,
            data: error.response.data,
          };
        } else if (error.request) {
          // Request was made but no response received
          throw new Error(
            `No response received from ${config.url}: ${error.message}`
          );
        }
      }
      throw error;
    }
  }

  /**
   * GET request
   */
  async get<T = any>(
    url: string,
    params?: Record<string, any>,
    headers?: Record<string, string>
  ): Promise<ApiResponse<T>> {
    return this.request<T>({
      method: 'GET',
      url,
      params,
      headers,
    });
  }

  /**
   * POST request
   */
  async post<T = any>(
    url: string,
    body?: any,
    headers?: Record<string, string>
  ): Promise<ApiResponse<T>> {
    return this.request<T>({
      method: 'POST',
      url,
      body,
      headers,
    });
  }

  /**
   * PUT request
   */
  async put<T = any>(
    url: string,
    body?: any,
    headers?: Record<string, string>
  ): Promise<ApiResponse<T>> {
    return this.request<T>({
      method: 'PUT',
      url,
      body,
      headers,
    });
  }

  /**
   * DELETE request
   */
  async delete<T = any>(
    url: string,
    headers?: Record<string, string>
  ): Promise<ApiResponse<T>> {
    return this.request<T>({
      method: 'DELETE',
      url,
      headers,
    });
  }

  /**
   * PATCH request
   */
  async patch<T = any>(
    url: string,
    body?: any,
    headers?: Record<string, string>
  ): Promise<ApiResponse<T>> {
    return this.request<T>({
      method: 'PATCH',
      url,
      body,
      headers,
    });
  }

  /**
   * Login to get authentication token
   * Note: This is a simplified login - actual implementation depends on auth flow
   */
  async login(credentials: LoginCredentials): Promise<string> {
    const response = await this.post('/api/v1/auth/login', credentials);

    if (response.status !== 200) {
      throw new Error(
        `Login failed with status ${response.status}: ${JSON.stringify(response.data)}`
      );
    }

    if (!response.data.token) {
      throw new Error('Login response missing token');
    }

    this.token = response.data.token;
    return this.token;
  }

  /**
   * Set authentication token manually
   */
  setToken(token: string): void {
    this.token = token;
  }

  /**
   * Get current authentication token
   */
  getToken(): string | null {
    return this.token;
  }

  /**
   * Clear authentication token
   */
  clearToken(): void {
    this.token = null;
  }

  /**
   * Check if client is authenticated
   */
  isAuthenticated(): boolean {
    return this.token !== null;
  }

  /**
   * Get base URL
   */
  getBaseURL(): string {
    return this.baseURL;
  }

  /**
   * Create a new client with same config but no auth
   */
  clone(): ApiClient {
    return new ApiClient(this.baseURL);
  }
}
