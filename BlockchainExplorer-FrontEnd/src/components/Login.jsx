import React, { useEffect, useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { useAuth } from '../auth/AuthContext';
import { SectionTitle } from './ui';

const DEMO_HINT = 'Demo: alice / password123 · bob / password123 · charlie / password123';

const Login = () => {
  const { login, isAuthenticated, loading, error } = useAuth();
  const navigate = useNavigate();
  const [username, setUsername] = useState('alice');
  const [password, setPassword] = useState('password123');
  const [localError, setLocalError] = useState('');

  useEffect(() => {
    if (isAuthenticated) navigate('/profile', { replace: true });
  }, [isAuthenticated, navigate]);

  const onSubmit = async (e) => {
    e.preventDefault();
    setLocalError('');
    try {
      await login(username.trim(), password);
      navigate('/profile');
    } catch (err) {
      setLocalError(err.message || 'Login failed');
    }
  };

  return (
    <div className="mx-auto max-w-md">
      <SectionTitle title="Sign in" subtitle="Demo wallet accounts seeded by Core Service" />
      <form onSubmit={onSubmit} className="mt-4 space-y-4">
        <label className="block">
          <span className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-[var(--muted)]">
            Username
          </span>
          <input
            className="explorer-input w-full"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            required
          />
        </label>
        <label className="block">
          <span className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-[var(--muted)]">
            Password
          </span>
          <input
            type="password"
            className="explorer-input w-full"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </label>
        {(localError || error) && (
          <p className="rounded-lg border border-[rgba(255,107,122,0.35)] bg-[rgba(255,107,122,0.08)] px-3 py-2 text-sm text-[var(--danger)]">
            {localError || error}
          </p>
        )}
        <button type="submit" disabled={loading} className="explorer-btn-primary w-full">
          {loading ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
      <p className="mt-4 text-xs leading-relaxed text-[var(--muted)]">{DEMO_HINT}</p>
      <p className="mt-3 text-xs text-[var(--muted)]">
        <Link to="/transactions" className="text-[var(--link)] hover:underline">
          ← Back to explorer
        </Link>
      </p>
    </div>
  );
};

export default Login;
