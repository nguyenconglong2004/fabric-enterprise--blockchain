import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { getMe, login as apiLogin, logout as apiLogout } from '../api/client';

const AuthContext = createContext(null);

const TOKEN_KEY = 'fabric_auth_token';
const USER_KEY = 'fabric_auth_username';

function readStoredToken() {
  return String(localStorage.getItem(TOKEN_KEY) || '')
    .trim()
    .replace(/^["']|["']$/g, '');
}

function readStoredUsername() {
  return String(localStorage.getItem(USER_KEY) || '').trim().toLowerCase();
}

export function AuthProvider({ children }) {
  const [token, setToken] = useState(() => readStoredToken());
  const [account, setAccount] = useState(null);
  const [loading, setLoading] = useState(() => !!readStoredToken());
  const [error, setError] = useState('');

  const clearSession = useCallback(() => {
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(USER_KEY);
    setToken('');
    setAccount(null);
  }, []);

  const persistSession = useCallback((tok, username) => {
    const clean = String(tok || '')
      .trim()
      .replace(/^["']|["']$/g, '');
    const user = String(username || '')
      .trim()
      .toLowerCase();
    if (!clean) return;
    localStorage.setItem(TOKEN_KEY, clean);
    if (user) localStorage.setItem(USER_KEY, user);
    setToken(clean);
  }, []);

  const refreshMe = useCallback(
    async (tok = token) => {
      const clean = String(tok || '')
        .trim()
        .replace(/^["']|["']$/g, '');
      if (!clean) {
        setAccount(null);
        setLoading(false);
        return null;
      }
      setLoading(true);
      setError('');
      try {
        const me = await getMe(clean);
        const meUser = String(me?.username || '')
          .trim()
          .toLowerCase();
        const expected = readStoredUsername();
        // If we remember who we logged in as, reject mismatch (stale token mix-up).
        if (expected && meUser && expected !== meUser) {
          clearSession();
          setError(`Session mismatch (expected ${expected}, got ${meUser}). Sign in again.`);
          return null;
        }
        if (meUser) localStorage.setItem(USER_KEY, meUser);
        setAccount(me);
        setToken(clean);
        return me;
      } catch (e) {
        clearSession();
        setError(e.message || 'Session expired');
        return null;
      } finally {
        setLoading(false);
      }
    },
    [token, clearSession]
  );

  useEffect(() => {
    const tok = readStoredToken();
    if (tok) refreshMe(tok);
    else setLoading(false);
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const login = useCallback(
    async (username, password) => {
      setLoading(true);
      setError('');
      try {
        // Drop any previous identity before writing the new one.
        clearSession();
        const res = await apiLogin(username, password);
        const tok = String(res.token || '')
          .trim()
          .replace(/^["']|["']$/g, '');
        if (!tok) throw new Error('Login response missing token');
        const user =
          res.account?.username ||
          username ||
          '';
        persistSession(tok, user);
        setAccount(res.account || null);
        return res;
      } catch (e) {
        clearSession();
        setError(e.message || 'Login failed');
        throw e;
      } finally {
        setLoading(false);
      }
    },
    [clearSession, persistSession]
  );

  const logout = useCallback(async () => {
    try {
      const tok = readStoredToken() || token;
      if (tok) await apiLogout(tok);
    } catch {
      // ignore
    }
    clearSession();
  }, [token, clearSession]);

  const value = useMemo(
    () => ({
      token,
      account,
      loading,
      error,
      login,
      logout,
      refreshMe,
      setAccount,
      isAuthenticated: !!token && !!account,
    }),
    [token, account, loading, error, login, logout, refreshMe]
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
