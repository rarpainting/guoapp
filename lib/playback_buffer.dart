import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:media_kit/media_kit.dart';

class PlaybackBufferRange {
  const PlaybackBufferRange(this.start, this.end);
  final double start, end;
}

List<PlaybackBufferRange> playbackBufferRanges(String raw) {
  try {
    final data = jsonDecode(raw) as Map;
    final ranges = <PlaybackBufferRange>[];
    for (final row in (data['seekable-ranges'] as List? ?? []).take(64)) {
      if (row is! Map || row['start'] is! num || row['end'] is! num) continue;
      final start = (row['start'] as num).toDouble(),
          end = (row['end'] as num).toDouble();
      if (!start.isFinite || !end.isFinite || start < 0 || end <= start) {
        continue;
      }
      ranges.add(PlaybackBufferRange(start, end));
    }
    ranges.sort((a, b) => a.start.compareTo(b.start));
    final merged = <PlaybackBufferRange>[];
    for (final range in ranges) {
      if (merged.isEmpty || range.start > merged.last.end) {
        merged.add(range);
      } else if (range.end > merged.last.end) {
        merged[merged.length - 1] = PlaybackBufferRange(
          merged.last.start,
          range.end,
        );
      }
    }
    return merged;
  } catch (_) {
    return const [];
  }
}

PlaybackBufferRange? continuousPlaybackBuffer(
  List<PlaybackBufferRange> ranges,
  double position,
) {
  if (!position.isFinite || position < 0) return null;
  for (final range in ranges) {
    if (position >= range.start && position <= range.end) return range;
  }
  return null;
}

String _clock(double seconds) {
  final value = seconds.floor().clamp(0, 864000);
  return '${value ~/ 60}:${(value % 60).toString().padLeft(2, '0')}';
}

class PlaybackBufferStatus extends StatefulWidget {
  const PlaybackBufferStatus({
    super.key,
    required this.player,
    this.enabled = true,
  });
  final Player player;
  final bool enabled;
  @override
  State<PlaybackBufferStatus> createState() => _PlaybackBufferStatusState();
}

class _PlaybackBufferStatusState extends State<PlaybackBufferStatus> {
  Timer? _timer;
  bool _reading = false;
  String _media = '';
  List<PlaybackBufferRange> _ranges = const [];

  @override
  void initState() {
    super.initState();
    _timer = Timer.periodic(const Duration(seconds: 1), (_) => _read());
  }

  Future<void> _read() async {
    final platform = widget.player.platform;
    if (_reading || !widget.enabled || platform is! NativePlayer) return;
    final media = widget.player.state.playlist.medias.firstOrNull?.uri ?? '';
    _reading = true;
    try {
      final raw = await platform
          .getProperty('demuxer-cache-state')
          .timeout(const Duration(seconds: 1));
      if (mounted &&
          media ==
              (widget.player.state.playlist.medias.firstOrNull?.uri ?? '')) {
        setState(() {
          _media = media;
          _ranges = playbackBufferRanges(raw);
        });
      }
    } catch (_) {
      if (mounted) setState(() => _ranges = const []);
    } finally {
      _reading = false;
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final state = widget.player.state;
    final position = state.position.inMilliseconds / 1000;
    final range = _media == (state.playlist.medias.firstOrNull?.uri ?? '')
        ? continuousPlaybackBuffer(_ranges, position)
        : null;
    final buffer = state.buffer.inMilliseconds / 1000;
    return Text(
      range == null
          ? buffer > position
                ? '已缓冲至 ${_clock(buffer)}'
                : '正在等待缓冲'
          : '连续缓存 ${(range.end - position).floor()} 秒 · ${_clock(range.start)}–${_clock(range.end)}',
      key: const ValueKey('playback-buffer-status'),
      style: const TextStyle(fontSize: 12, color: Colors.white70),
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
    );
  }
}
