import type { 
  Article, 
  Category, 
  Author, 
  Media, 
  Settings, 
  CreateArticleInput, 
  UpdateArticleInput,
  CreateCategoryInput,
  UpdateCategoryInput,
  PaginatedResponse,
  LoginResponse 
} from "@technews/shared";

const API_URL = '';

// Auth token management
let authToken: string | null = null;

export function setAuthToken(token: string | null) {
  authToken = token;
  if (token) {
    localStorage.setItem('auth_token', token);
  } else {
    localStorage.removeItem('auth_token');
  }
}

export function getAuthToken(): string | null {
  if (typeof window === 'undefined') return null;
  return authToken || localStorage.getItem('auth_token');
}

// Initialize auth token from localStorage on client-side
if (typeof window !== 'undefined') {
  authToken = localStorage.getItem('auth_token');
}

// Generic API call helper
async function apiCall<T>(endpoint: string, options: RequestInit = {}): Promise<T> {
  const url = `${API_URL}/api${endpoint}`;
  const token = getAuthToken();
  
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string> || {}),
  };
  
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  
  const response = await fetch(url, {
    ...options,
    headers,
  });
  
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: 'Network error' }));
    throw new Error(error.error || `HTTP ${response.status}`);
  }
  
  return response.json();
}

// Auth API
export const authApi = {
  async login(email: string, password: string): Promise<LoginResponse> {
    const response = await apiCall<LoginResponse>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    });
    setAuthToken(response.token);
    return response;
  },
  
  async me(): Promise<{ user: Author }> {
    return apiCall('/auth/me');
  },
  
  async logout(): Promise<{ success: boolean }> {
    const result = await apiCall<{ success: boolean }>('/auth/logout', {
      method: 'POST',
    });
    setAuthToken(null);
    return result;
  },
};

// Articles API
export const articlesApi = {
  // Dashboard endpoints
  async list(params?: {
    page?: number;
    limit?: number;
    status?: string;
    category?: string;
    search?: string;
  }): Promise<PaginatedResponse<Article & { author: Author; category: Category }>> {
    const searchParams = new URLSearchParams();
    if (params?.page) searchParams.set('page', params.page.toString());
    if (params?.limit) searchParams.set('limit', params.limit.toString());
    if (params?.status) searchParams.set('status', params.status);
    if (params?.category) searchParams.set('category', params.category);
    if (params?.search) searchParams.set('search', params.search);
    
    const query = searchParams.toString();
    return apiCall(`/dashboard/articles${query ? `?${query}` : ''}`);
  },
  
  async getById(id: number): Promise<{ article: Article & { author: Author; category: Category } }> {
    return apiCall(`/dashboard/articles/${id}`);
  },
  
  async create(data: CreateArticleInput): Promise<{ article: Article & { author: Author; category: Category } }> {
    return apiCall('/dashboard/articles', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },
  
  async update(id: number, data: UpdateArticleInput): Promise<{ article: Article & { author: Author; category: Category } }> {
    return apiCall(`/dashboard/articles/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  },
  
  async delete(id: number): Promise<{ success: boolean }> {
    return apiCall(`/dashboard/articles/${id}`, {
      method: 'DELETE',
    });
  },
  
  // Public endpoints
  async getPublicById(id: number): Promise<{ article: Article & { author: Author; category: Category } }> {
    return apiCall(`/articles/id/${id}`);
  },
};

// Categories API
export const categoriesApi = {
  async list(): Promise<{ categories: Category[] }> {
    return apiCall('/dashboard/categories');
  },
  
  async create(data: CreateCategoryInput): Promise<{ category: Category }> {
    return apiCall('/dashboard/categories', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },
  
  async update(id: number, data: UpdateCategoryInput): Promise<{ category: Category }> {
    return apiCall(`/dashboard/categories/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  },
  
  async delete(id: number): Promise<{ success: boolean }> {
    return apiCall(`/dashboard/categories/${id}`, {
      method: 'DELETE',
    });
  },
  
  // Public endpoint
  async getPublic(): Promise<{ categories: Category[] }> {
    return apiCall('/categories');
  },
};

// Authors API
export const authorsApi = {
  async list(): Promise<{ authors: Author[] }> {
    return apiCall('/authors');
  },
};

// Media API
export const mediaApi = {
  async list(): Promise<{ media: Media[] }> {
    return apiCall('/dashboard/media');
  },
  
  async upload(file: File): Promise<{ media: Media }> {
    const formData = new FormData();
    formData.append('file', file);
    
    const token = getAuthToken();
    const response = await fetch(`${API_URL}/api/dashboard/media/upload`, {
      method: 'POST',
      headers: {
        ...(token && { Authorization: `Bearer ${token}` }),
      },
      body: formData,
    });
    
    if (!response.ok) {
      const error = await response.json().catch(() => ({ error: 'Upload failed' }));
      throw new Error(error.error || `HTTP ${response.status}`);
    }
    
    return response.json();
  },
  
  async delete(id: number): Promise<{ success: boolean }> {
    return apiCall(`/dashboard/media/${id}`, {
      method: 'DELETE',
    });
  },
};

// Settings API
export const settingsApi = {
  async get(): Promise<{ settings: Settings }> {
    return apiCall('/dashboard/settings');
  },
  
  async update(settings: Partial<Settings>): Promise<{ settings: Settings }> {
    return apiCall('/dashboard/settings', {
      method: 'PUT',
      body: JSON.stringify(settings),
    });
  },
};