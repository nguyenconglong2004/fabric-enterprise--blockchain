import React from 'react';
import { BrowserRouter as Router, Routes, Route, Navigate } from 'react-router-dom';
import Dashboard from './components/Dashboard';
import { AuthProvider } from './auth/AuthContext';

function App() {
  return (
    <AuthProvider>
      <Router>
        <div className="min-h-screen">
          <Routes>
            <Route path="/" element={<Navigate to="/transactions" replace />} />
            <Route path="/transactions" element={<Dashboard section="transactions" />} />
            <Route path="/transfer" element={<Dashboard section="transfer" />} />
            <Route path="/blocks" element={<Dashboard section="blocks" />} />
            <Route path="/login" element={<Dashboard section="login" />} />
            <Route path="/profile" element={<Dashboard section="profile" />} />
          </Routes>
        </div>
      </Router>
    </AuthProvider>
  );
}

export default App;
