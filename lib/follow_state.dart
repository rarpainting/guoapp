import 'dart:math';

import 'models.dart';

enum FollowStatus {
  planned('想看'),
  watching('在看'),
  watched('已看');

  const FollowStatus(this.label);
  final String label;
}

class FollowState {
  const FollowState({
    this.status = FollowStatus.planned,
    this.manuallyWatched = false,
    this.knownEpisodes = 0,
    this.readEpisodes,
  });

  final FollowStatus status;
  final bool manuallyWatched;
  final int knownEpisodes;
  final int? readEpisodes;

  int get newEpisodes =>
      readEpisodes == null ? 0 : max(0, knownEpisodes - readEpisodes!);
  String get label => manuallyWatched ? '已看 · 手动标记' : status.label;

  factory FollowState.initial(Drama drama, WatchEntry? watch) {
    final count = max(0, drama.episodes);
    final state = FollowState(
      knownEpisodes: count,
      readEpisodes: count > 0 ? count : null,
    );
    return watch == null ? state : state.afterPlayback(watch);
  }

  FollowState observe(Drama drama) {
    final count = max(knownEpisodes, drama.episodes);
    return FollowState(
      status:
          !manuallyWatched &&
              status == FollowStatus.watched &&
              count > knownEpisodes
          ? FollowStatus.watching
          : status,
      manuallyWatched: manuallyWatched,
      knownEpisodes: count,
      readEpisodes: readEpisodes ?? (count > 0 ? count : null),
    );
  }

  FollowState afterPlayback(WatchEntry entry) {
    final state = observe(entry.drama);
    if (state.manuallyWatched || entry.position <= 0) return state;
    return FollowState(
      status:
          state.knownEpisodes > 0 &&
              entry.finished &&
              entry.episode >= state.knownEpisodes
          ? FollowStatus.watched
          : FollowStatus.watching,
      knownEpisodes: state.knownEpisodes,
      readEpisodes: state.readEpisodes,
    );
  }

  FollowState withStatus(FollowStatus value) => FollowState(
    status: value,
    manuallyWatched: value == FollowStatus.watched,
    knownEpisodes: knownEpisodes,
    readEpisodes: value == FollowStatus.watched && knownEpisodes > 0
        ? knownEpisodes
        : readEpisodes,
  );

  FollowState markRead() => FollowState(
    status: status,
    manuallyWatched: manuallyWatched,
    knownEpisodes: knownEpisodes,
    readEpisodes: knownEpisodes > 0 ? knownEpisodes : null,
  );

  Map<String, dynamic> toJson() => {
    'status': status.name,
    'manuallyWatched': manuallyWatched,
    'knownEpisodes': knownEpisodes,
    'readEpisodes': readEpisodes,
  };

  factory FollowState.fromJson(Map<String, dynamic> json) {
    final status = FollowStatus.values
        .where((value) => value.name == json['status'])
        .firstOrNull;
    final known = json['knownEpisodes'];
    final read = json['readEpisodes'];
    final manual = json['manuallyWatched'];
    if (status == null ||
        known is! int ||
        known < 0 ||
        known > 1000000 ||
        read != null && (read is! int || read < 0 || read > known) ||
        manual is! bool ||
        manual && status != FollowStatus.watched) {
      throw const FormatException('追剧状态无效');
    }
    return FollowState(
      status: status,
      manuallyWatched: manual,
      knownEpisodes: known,
      readEpisodes: read as int?,
    );
  }
}

int resumeEpisodeIndex(List<Episode> episodes, WatchEntry? watch) {
  if (episodes.isEmpty || watch == null) return 0;
  final current = episodes.indexWhere((item) => item.number == watch.episode);
  if (current >= 0 && !watch.finished) return current;
  final next = episodes.indexWhere((item) => item.number > watch.episode);
  if (next >= 0) return next;
  return current >= 0 ? current : episodes.length - 1;
}
