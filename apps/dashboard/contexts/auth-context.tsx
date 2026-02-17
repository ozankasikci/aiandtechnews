"use client";

import { createContext, useContext, useEffect, useState } from "react";
import { useRouter, usePathname } from "next/navigation";
import type { Author } from "@technews/shared";
import { authApi, getAuthToken } from "@/lib/api";

interface AuthContextType {
  user: Author | null;
  loading: boolean;
  login: (email: string, password: string) => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<Author | null>(null);
  const [loading, setLoading] = useState(true);
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    const initAuth = async () => {
      const token = getAuthToken();
      
      if (!token) {
        setLoading(false);
        if (pathname !== '/login') {
          router.push('/login');
        }
        return;
      }
      
      try {
        const { user } = await authApi.me();
        // Add missing fields if needed
        setUser({ 
          ...user, 
          avatar: user.avatar || null, 
          bio: user.bio || null 
        });
        
        // Redirect from login page if already authenticated
        if (pathname === '/login') {
          router.push('/articles');
        }
      } catch (error) {
        console.error('Auth check failed:', error);
        // Token is invalid, remove it
        await logout();
      }
      
      setLoading(false);
    };

    initAuth();
  }, [router, pathname]);

  const login = async (email: string, password: string) => {
    try {
      const response = await authApi.login(email, password);
      // Add missing fields for the user state
      setUser({ 
        ...response.user, 
        avatar: null, 
        bio: null 
      });
      router.push('/articles');
    } catch (error) {
      throw error; // Re-throw so component can handle it
    }
  };

  const logout = async () => {
    try {
      await authApi.logout();
    } catch (error) {
      // Ignore logout API errors, still clear local state
      console.warn('Logout API call failed:', error);
    } finally {
      setUser(null);
      router.push('/login');
    }
  };

  const value = {
    user,
    loading,
    login,
    logout,
  };

  return (
    <AuthContext.Provider value={value}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
}