// Curly Chainsaw — Mobile App
//
// React Native (Expo) app for iOS and Android.
// Bottom tab navigation: Dashboard, Search, Matches, Profile.

import React, { useEffect, useState } from 'react';
import {
  View,
  Text,
  TextInput,
  FlatList,
  TouchableOpacity,
  StyleSheet,
  StatusBar,
  ActivityIndicator,
  RefreshControl,
} from 'react-native';
import { api, Job, MatchResult, Stats } from './api';

// ─── Theme ───
const colors = {
  bg: '#050a12',
  card: '#111d30',
  border: '#1e293b',
  green: '#10b981',
  cyan: '#06b6d4',
  orange: '#f59e0b',
  white: '#f1f5f9',
  gray: '#94a3b8',
  muted: '#64748b',
};

// ─── Dashboard Screen ───
function DashboardScreen() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [matches, setMatches] = useState<MatchResult[]>([]);
  const [loading, setLoading] = useState(false);

  const refresh = async () => {
    setLoading(true);
    try {
      const [s, m] = await Promise.all([
        api.stats().catch(() => null),
        api.matches().catch(() => []),
      ]);
      if (s) setStats(s);
      setMatches(m);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { refresh(); }, []);

  return (
    <View style={styles.screen}>
      <Text style={styles.title}>Dashboard</Text>

      {/* Stats */}
      <View style={styles.statsRow}>
        <StatCard label="Discovered" value={stats?.jobs_discovered ?? 0} color={colors.green} />
        <StatCard label="Matches" value={stats?.matches_found ?? 0} color={colors.cyan} />
        <StatCard label="Applied" value={stats?.applications_sent ?? 0} color={colors.orange} />
      </View>

      {/* Discover button */}
      <TouchableOpacity
        style={styles.discoverBtn}
        onPress={async () => {
          setLoading(true);
          try {
            await api.discover();
            await refresh();
          } finally {
            setLoading(false);
          }
        }}
      >
        {loading ? (
          <ActivityIndicator color="#000" />
        ) : (
          <Text style={styles.discoverBtnText}>Auto-Discover</Text>
        )}
      </TouchableOpacity>

      {/* Recent matches */}
      <Text style={styles.sectionHeader}>Top Matches</Text>
      <FlatList
        data={matches.slice(0, 20)}
        keyExtractor={(item) => item.job.id}
        refreshControl={<RefreshControl refreshing={loading} onRefresh={refresh} tintColor={colors.green} />}
        renderItem={({ item }) => <JobCard job={item.job} score={item.score} reason={item.reason} />}
        ListEmptyComponent={
          <View style={styles.empty}>
            <Text style={styles.emptyText}>No matches yet. Hit Auto-Discover to start chopping.</Text>
          </View>
        }
      />
    </View>
  );
}

// ─── Search Screen ───
function SearchScreen() {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<Job[]>([]);
  const [loading, setLoading] = useState(false);

  const doSearch = async () => {
    if (!query.trim()) return;
    setLoading(true);
    try {
      const jobs = await api.search(query);
      setResults(jobs);
    } finally {
      setLoading(false);
    }
  };

  return (
    <View style={styles.screen}>
      <Text style={styles.title}>Search</Text>
      <View style={styles.searchRow}>
        <TextInput
          style={styles.input}
          placeholder="remote python engineer..."
          placeholderTextColor={colors.muted}
          value={query}
          onChangeText={setQuery}
          onSubmitEditing={doSearch}
          returnKeyType="search"
        />
        <TouchableOpacity style={styles.searchBtn} onPress={doSearch}>
          <Text style={styles.searchBtnText}>Go</Text>
        </TouchableOpacity>
      </View>

      {loading && <ActivityIndicator color={colors.green} style={{ marginTop: 24 }} />}

      <FlatList
        data={results}
        keyExtractor={(item) => item.id}
        renderItem={({ item }) => <JobCard job={item} />}
        ListEmptyComponent={
          !loading ? (
            <View style={styles.empty}>
              <Text style={styles.emptyText}>
                Search across Greenhouse, Ashby, Workday, and more.
              </Text>
            </View>
          ) : null
        }
      />
    </View>
  );
}

// ─── Shared Components ───
function StatCard({ label, value, color }: { label: string; value: number; color: string }) {
  return (
    <View style={styles.statCard}>
      <Text style={styles.statLabel}>{label}</Text>
      <Text style={[styles.statValue, { color }]}>{value}</Text>
    </View>
  );
}

function trustGradeColor(grade?: string): string {
  switch (grade) {
    case 'A': return colors.green;
    case 'B': return colors.cyan;
    case 'C': return colors.orange;
    default:  return '#ef4444';
  }
}

function JobCard({ job, score, reason }: { job: Job; score?: number; reason?: string }) {
  return (
    <View style={styles.jobCard}>
      <View style={styles.jobHeader}>
        <View style={{ flex: 1 }}>
          <Text style={styles.jobTitle}>{job.title}</Text>
          <Text style={styles.jobCompany}>{job.company}</Text>
        </View>
        {score != null && <Text style={styles.matchScore}>{Math.round(score * 100)}%</Text>}
      </View>
      <View style={styles.badgeRow}>
        {job.remote && <Badge text="REMOTE" color={colors.green} />}
        {job.platform && <Badge text={job.platform} color={colors.orange} />}
        {job.company_trust_grade && (
          <Badge
            text={`TRUST ${job.company_trust_grade} (${job.company_trust_score})`}
            color={trustGradeColor(job.company_trust_grade)}
          />
        )}
        {reason && <Badge text={reason} color={colors.cyan} />}
      </View>
    </View>
  );
}

function Badge({ text, color }: { text: string; color: string }) {
  return (
    <View style={[styles.badge, { borderColor: color }]}>
      <Text style={[styles.badgeText, { color }]}>{text}</Text>
    </View>
  );
}

// ─── Main App (simplified — in production this uses @react-navigation) ───
export default function App() {
  const [tab, setTab] = useState<'dashboard' | 'search'>('dashboard');

  return (
    <View style={{ flex: 1, backgroundColor: colors.bg }}>
      <StatusBar barStyle="light-content" />
      {tab === 'dashboard' ? <DashboardScreen /> : <SearchScreen />}

      {/* Bottom tab bar */}
      <View style={styles.tabBar}>
        <TouchableOpacity style={styles.tab} onPress={() => setTab('dashboard')}>
          <Text style={[styles.tabText, tab === 'dashboard' && styles.tabActive]}>Dashboard</Text>
        </TouchableOpacity>
        <TouchableOpacity style={styles.tab} onPress={() => setTab('search')}>
          <Text style={[styles.tabText, tab === 'search' && styles.tabActive]}>Search</Text>
        </TouchableOpacity>
      </View>
    </View>
  );
}

// ─── Styles ───
const styles = StyleSheet.create({
  screen: { flex: 1, padding: 20, paddingTop: 60 },
  title: { fontSize: 28, fontWeight: '800', color: colors.white, marginBottom: 20 },
  sectionHeader: { fontSize: 16, fontWeight: '600', color: colors.gray, marginTop: 24, marginBottom: 12 },

  statsRow: { flexDirection: 'row', gap: 12, marginBottom: 16 },
  statCard: {
    flex: 1, backgroundColor: colors.card, borderRadius: 10, padding: 14,
    borderWidth: 1, borderColor: colors.border,
  },
  statLabel: { fontSize: 11, color: colors.muted, textTransform: 'uppercase', letterSpacing: 0.5 },
  statValue: { fontSize: 24, fontWeight: '700', marginTop: 4 },

  discoverBtn: {
    backgroundColor: colors.green, borderRadius: 10, padding: 14, alignItems: 'center',
  },
  discoverBtnText: { color: '#000', fontWeight: '700', fontSize: 16 },

  searchRow: { flexDirection: 'row', gap: 8, marginBottom: 16 },
  input: {
    flex: 1, backgroundColor: colors.card, borderRadius: 8, padding: 12,
    color: colors.white, fontSize: 15, borderWidth: 1, borderColor: colors.border,
  },
  searchBtn: { backgroundColor: colors.green, borderRadius: 8, paddingHorizontal: 20, justifyContent: 'center' },
  searchBtnText: { color: '#000', fontWeight: '700' },

  jobCard: {
    backgroundColor: colors.card, borderRadius: 10, padding: 14, marginBottom: 10,
    borderWidth: 1, borderColor: colors.border,
  },
  jobHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'flex-start' },
  jobTitle: { fontSize: 16, fontWeight: '600', color: colors.white },
  jobCompany: { fontSize: 14, color: colors.gray, marginTop: 2 },
  matchScore: { fontSize: 20, fontWeight: '700', color: colors.cyan },
  badgeRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 6, marginTop: 8 },
  badge: { borderWidth: 1, borderRadius: 4, paddingHorizontal: 8, paddingVertical: 2 },
  badgeText: { fontSize: 11, fontWeight: '600' },

  empty: { alignItems: 'center', paddingVertical: 40 },
  emptyText: { color: colors.muted, fontSize: 14 },

  tabBar: {
    flexDirection: 'row', borderTopWidth: 1, borderTopColor: colors.border,
    backgroundColor: colors.card, paddingBottom: 20,
  },
  tab: { flex: 1, alignItems: 'center', paddingVertical: 12 },
  tabText: { fontSize: 13, color: colors.muted, fontWeight: '600' },
  tabActive: { color: colors.green },
});
