import 'dart:async';

import 'package:flutter/material.dart';
import 'package:media_kit/media_kit.dart';

import 'player_interactions.dart';
import 'playback_buffer.dart';
import 'widgets.dart';
import 'lan_screen.dart';
import 'video_enhancement.dart';

class PlayerControls extends StatefulWidget {
  const PlayerControls({
    super.key,
    required this.player,
    required this.interactions,
    required this.enabled,
    required this.fullscreen,
    required this.showOnPlaybackReady,
    required this.onFullscreen,
    required this.onPrevious,
    required this.onNext,
    required this.title,
    required this.onTogglePlayback,
    required this.onEpisodes,
    required this.onSettings,
    required this.onSpeed,
    required this.onQuality,
    required this.speed,
    required this.qualityLabel,
    required this.onFocusSurface,
    this.swipeEnabled = false,
    this.panelOpen = false,
    this.onSeek,
    this.onPush,
    this.onPictureInPicture,
    this.enhancement,
  });

  final Player player;
  final PlayerInteractions interactions;
  final bool enabled;
  final bool fullscreen;
  final bool showOnPlaybackReady;
  final VoidCallback onFullscreen;
  final VoidCallback? onPrevious;
  final VoidCallback? onNext;
  final String title;
  final VoidCallback onTogglePlayback;
  final Future<void> Function() onEpisodes;
  final Future<void> Function() onSettings;
  final Future<void> Function() onSpeed;
  final Future<void> Function() onQuality;
  final double speed;
  final String qualityLabel;
  final VoidCallback onFocusSurface;
  final bool swipeEnabled;
  final bool panelOpen;
  final Future<void> Function(Duration)? onSeek;
  final Future<void> Function()? onPush;
  final Future<void> Function()? onPictureInPicture;
  final VideoEnhancementController? enhancement;

  @override
  State<PlayerControls> createState() => _PlayerControlsState();
}

class _PlayerControlsState extends State<PlayerControls> {
  final List<StreamSubscription<dynamic>> _subscriptions = [];
  Timer? _hideTimer;
  bool _visible = true;
  bool _suppressAutoPlaybackStart = false;
  bool _lastPlaying = false;
  double? _seekValue;

  @override
  void initState() {
    super.initState();
    _visible = widget.showOnPlaybackReady;
    _suppressAutoPlaybackStart = !widget.showOnPlaybackReady;
    _lastPlaying = widget.player.state.playing;
    for (final stream in [
      widget.player.stream.position,
      widget.player.stream.duration,
      widget.player.stream.buffer,
      widget.player.stream.playing,
      widget.player.stream.buffering,
      widget.player.stream.volume,
    ]) {
      _subscriptions.add(
        stream.listen((_) {
          if (mounted) setState(() {});
        }),
      );
    }
    _subscriptions.add(
      widget.player.stream.playing.listen((playing) {
        final wasPlaying = _lastPlaying;
        _lastPlaying = playing;
        if (!mounted || !widget.enabled) return;
        if (!playing) {
          _show();
        } else if (!wasPlaying) {
          if (_suppressAutoPlaybackStart) {
            _suppressAutoPlaybackStart = false;
            _scheduleHide();
          } else {
            _show();
          }
        }
      }),
    );
    widget.interactions.addListener(_interactionChanged);
    _scheduleHide();
  }

  @override
  void didUpdateWidget(PlayerControls oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.enabled != oldWidget.enabled ||
        widget.panelOpen != oldWidget.panelOpen ||
        widget.fullscreen != oldWidget.fullscreen ||
        widget.showOnPlaybackReady != oldWidget.showOnPlaybackReady) {
      _seekValue = null;
      if (widget.showOnPlaybackReady) {
        _suppressAutoPlaybackStart = false;
      } else if (widget.enabled && !oldWidget.enabled) {
        _suppressAutoPlaybackStart = !widget.player.state.playing;
      } else if (!widget.enabled && oldWidget.enabled) {
        _suppressAutoPlaybackStart = true;
        _visible = false;
      }
      if (widget.enabled && !oldWidget.enabled) {
        if (widget.showOnPlaybackReady || !widget.player.state.playing) {
          _visible = true;
        } else {
          _visible = false;
        }
      } else if (widget.panelOpen != oldWidget.panelOpen ||
          widget.fullscreen != oldWidget.fullscreen) {
        _visible = true;
      }
      _scheduleHide();
    }
  }

  void _interactionChanged() {
    if (mounted && widget.interactions.feedback.isNotEmpty) _show();
  }

  void _scheduleHide() {
    _hideTimer?.cancel();
    if (!widget.enabled ||
        widget.panelOpen ||
        !widget.player.state.playing ||
        _seekValue != null) {
      return;
    }
    _hideTimer = Timer(const Duration(seconds: 4), () {
      if (mounted &&
          widget.enabled &&
          !widget.panelOpen &&
          widget.player.state.playing &&
          !widget.player.state.buffering &&
          !widget.interactions.boosting &&
          _seekValue == null) {
        setState(() => _visible = false);
      }
    });
  }

  void _show() {
    if (!mounted) return;
    if (!_visible) setState(() => _visible = true);
    _scheduleHide();
  }

  void _tap() {
    if (widget.interactions.suppressTap) return;
    widget.onFocusSurface();
    if (widget.swipeEnabled &&
        MediaQuery.orientationOf(context) == Orientation.portrait &&
        widget.enabled) {
      widget.onTogglePlayback();
      _show();
    } else {
      setState(() => _visible = !_visible);
      _scheduleHide();
    }
  }

  Future<void> _panel(Future<void> Function() open) async {
    _hideTimer?.cancel();
    widget.interactions.cancel();
    await open();
    _show();
  }

  @override
  void dispose() {
    _hideTimer?.cancel();
    widget.interactions.removeListener(_interactionChanged);
    for (final subscription in _subscriptions) {
      subscription.cancel();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final state = widget.player.state;
    final duration = state.duration.inMilliseconds / 1000;
    final position = state.position.inMilliseconds / 1000;
    final buffered = state.buffer.inMilliseconds / 1000;
    final visible =
        _visible || !state.playing || state.buffering || widget.panelOpen;
    return MouseRegion(
      onHover: (_) => _show(),
      cursor: visible ? SystemMouseCursors.basic : SystemMouseCursors.none,
      child: LayoutBuilder(
        builder: (context, constraints) => Stack(
          fit: StackFit.expand,
          children: [
            Listener(
              key: const ValueKey('player-gesture-surface'),
              behavior: HitTestBehavior.opaque,
              onPointerDown: (event) {
                widget.onFocusSurface();
                widget.interactions.pointerDown(
                  event,
                  swipeEnabled: widget.swipeEnabled,
                  height: constraints.maxHeight,
                );
              },
              onPointerMove: widget.interactions.pointerMove,
              onPointerUp: widget.interactions.pointerUp,
              onPointerCancel: widget.interactions.pointerCancel,
              child: GestureDetector(
                behavior: HitTestBehavior.opaque,
                onTap: _tap,
                onDoubleTap: () {
                  if (!widget.enabled || widget.interactions.suppressTap) {
                    return;
                  }
                  widget.onTogglePlayback();
                  _show();
                },
              ),
            ),
            if (state.buffering && widget.enabled)
              const IgnorePointer(
                child: Center(child: CircularProgressIndicator()),
              ),
            IgnorePointer(
              ignoring: !visible,
              child: ExcludeFocus(
                excluding: !visible,
                child: AnimatedOpacity(
                  opacity: visible ? 1 : 0,
                  duration: const Duration(milliseconds: 180),
                  child: Stack(
                    fit: StackFit.expand,
                    children: [
                      const IgnorePointer(
                        child: DecoratedBox(
                          decoration: BoxDecoration(
                            gradient: LinearGradient(
                              begin: Alignment.topCenter,
                              end: Alignment.bottomCenter,
                              colors: [
                                Color(0xAA000000),
                                Colors.transparent,
                                Color(0xE6000000),
                              ],
                              stops: [0, .45, 1],
                            ),
                          ),
                        ),
                      ),
                      SafeArea(
                        top: widget.fullscreen,
                        bottom: widget.fullscreen,
                        minimum: const EdgeInsets.symmetric(horizontal: 8),
                        child: Stack(
                          children: [
                            if (widget.fullscreen) _topBar(),
                            if (!widget.fullscreen &&
                                !state.buffering &&
                                widget.enabled &&
                                constraints.maxHeight >= 300)
                              _centerPlayback(state.playing),
                            _bottomControls(
                              constraints: constraints,
                              playing: state.playing,
                              volume: state.volume,
                              duration: duration,
                              position: position,
                              buffered: buffered,
                            ),
                          ],
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ),
            _gestureFeedback(),
          ],
        ),
      ),
    );
  }

  Widget _topBar() => Align(
    alignment: Alignment.topCenter,
    child: Row(
      children: [
        IconButton(
          tooltip: '退出全屏',
          onPressed: widget.onFullscreen,
          icon: const Icon(Icons.arrow_back_rounded),
        ),
        Expanded(
          child: Text(
            widget.title,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
          ),
        ),
      ],
    ),
  );

  Widget _centerPlayback(bool playing) => Center(
    child: Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        IconButton(
          tooltip: '上一集',
          onPressed: widget.onPrevious,
          icon: const Icon(Icons.skip_previous_rounded),
        ),
        const SizedBox(width: 12),
        IconButton.filledTonal(
          tooltip: playing ? '暂停' : '播放',
          iconSize: 38,
          onPressed: () {
            widget.onTogglePlayback();
            _show();
          },
          icon: Icon(playing ? Icons.pause_rounded : Icons.play_arrow_rounded),
        ),
        const SizedBox(width: 12),
        IconButton(
          tooltip: '下一集',
          onPressed: widget.onNext,
          icon: const Icon(Icons.skip_next_rounded),
        ),
      ],
    ),
  );

  Widget _bottomControls({
    required BoxConstraints constraints,
    required bool playing,
    required double volume,
    required double duration,
    required double position,
    required double buffered,
  }) {
    final width = constraints.maxWidth;
    final fullscreen = widget.fullscreen;
    final showSkip = width >= 380;
    final showEpisodes = fullscreen || width >= 420;
    final showSpeedQuality = fullscreen && width >= 720;
    final showPush = widget.onPush != null && fullscreen && width >= 860;
    final showCompare = fullscreen && width >= 820;
    final showVolume = !widget.swipeEnabled && width >= 520;
    return Align(
      alignment: Alignment.bottomCenter,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          SliderTheme(
            data: SliderTheme.of(context).copyWith(trackHeight: 3),
            child: Slider(
              key: const ValueKey('player-progress'),
              value: (_seekValue ?? position).clamp(
                0,
                duration > 0 ? duration : 1,
              ),
              max: duration > 0 ? duration : 1,
              secondaryTrackValue: buffered.clamp(
                0,
                duration > 0 ? duration : 1,
              ),
              semanticFormatterCallback: formatPosition,
              onChangeStart: widget.enabled && duration > 0
                  ? (_) {
                      widget.interactions.cancel();
                      _hideTimer?.cancel();
                    }
                  : null,
              onChanged: !widget.enabled || duration <= 0
                  ? null
                  : (value) {
                      _hideTimer?.cancel();
                      setState(() => _seekValue = value);
                    },
              onChangeEnd: (value) {
                if (widget.enabled && duration > 0) {
                  (widget.onSeek ?? widget.player.seek)(
                    Duration(milliseconds: (value * 1000).round()),
                  );
                }
                setState(() => _seekValue = null);
                _show();
              },
            ),
          ),
          if (constraints.maxHeight >= 240)
            PlaybackBufferStatus(
              player: widget.player,
              enabled: widget.enabled,
            ),
          Row(
            children: [
              IconButton(
                tooltip: playing ? '暂停' : '播放',
                onPressed: widget.enabled
                    ? () {
                        widget.onTogglePlayback();
                        _show();
                      }
                    : null,
                icon: Icon(
                  playing ? Icons.pause_rounded : Icons.play_arrow_rounded,
                ),
              ),
              if (showSkip) ...[
                IconButton(
                  tooltip: '上一集',
                  onPressed: widget.onPrevious,
                  icon: const Icon(Icons.skip_previous_rounded),
                ),
                IconButton(
                  tooltip: '下一集',
                  onPressed: widget.onNext,
                  icon: const Icon(Icons.skip_next_rounded),
                ),
              ],
              Expanded(
                child: Text(
                  '${formatPosition(_seekValue ?? position)} / ${formatPosition(duration)}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(fontSize: 12),
                ),
              ),
              if (showVolume)
                IconButton(
                  tooltip: volume == 0 ? '取消静音' : '静音',
                  onPressed: widget.enabled
                      ? widget.interactions.toggleMute
                      : null,
                  icon: Icon(
                    volume == 0
                        ? Icons.volume_off_rounded
                        : Icons.volume_up_rounded,
                  ),
                ),
              if (showEpisodes)
                IconButton(
                  key: const ValueKey('player-episodes'),
                  tooltip: '选集',
                  onPressed: widget.enabled
                      ? () => _panel(widget.onEpisodes)
                      : null,
                  icon: const Icon(Icons.grid_view_rounded),
                ),
              if (showSpeedQuality) ...[
                TextButton(
                  key: const ValueKey('player-speed'),
                  onPressed: widget.enabled
                      ? () => _panel(widget.onSpeed)
                      : null,
                  child: Text('${widget.speed}x'),
                ),
                TextButton(
                  key: const ValueKey('player-quality'),
                  onPressed: widget.enabled
                      ? () => _panel(widget.onQuality)
                      : null,
                  child: Text(widget.qualityLabel),
                ),
              ],
              if (showPush)
                LanPushButton(
                  key: const ValueKey('fullscreen-lan-push'),
                  onPressed: widget.enabled
                      ? () => _panel(widget.onPush!)
                      : null,
                ),
              if (showCompare) _enhancementCompareButton(),
              if (widget.onPictureInPicture != null)
                IconButton(
                  key: const ValueKey('player-picture-in-picture'),
                  tooltip: '画中画',
                  onPressed: widget.enabled
                      ? () => _panel(widget.onPictureInPicture!)
                      : null,
                  icon: const Icon(Icons.picture_in_picture_alt_rounded),
                ),
              IconButton(
                key: const ValueKey('player-settings'),
                tooltip: '播放设置',
                onPressed: widget.enabled
                    ? () => _panel(widget.onSettings)
                    : null,
                icon: const Icon(Icons.tune_rounded),
              ),
              IconButton(
                tooltip: fullscreen ? '退出全屏' : '旋转与全屏',
                onPressed: widget.onFullscreen,
                icon: Icon(
                  fullscreen
                      ? Icons.fullscreen_exit_rounded
                      : Icons.fullscreen_rounded,
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _enhancementCompareButton() {
    final enhancement = widget.enhancement;
    if (enhancement == null) return const SizedBox.shrink();
    return AnimatedBuilder(
      animation: enhancement,
      builder: (_, _) {
        if (!enhancement.canCompare) return const SizedBox.shrink();
        return IconButton(
          key: const ValueKey('player-enhancement-compare'),
          tooltip: enhancement.comparing ? '原画对比中，点击恢复增强' : '原画对比',
          isSelected: enhancement.comparing,
          onPressed: widget.enabled
              ? () {
                  widget.interactions.cancel();
                  unawaited(enhancement.toggleCompare());
                  _show();
                }
              : null,
          icon: const Icon(Icons.compare_rounded),
        );
      },
    );
  }

  Widget _gestureFeedback() => AnimatedBuilder(
    animation: widget.interactions,
    builder: (context, _) {
      final feedback = widget.interactions.feedback;
      if (feedback.isEmpty) return const SizedBox.shrink();
      return IgnorePointer(
        child: Align(
          alignment: const Alignment(0, -.5),
          child: Container(
            margin: const EdgeInsets.all(16),
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
            decoration: BoxDecoration(
              color: Colors.black87,
              borderRadius: BorderRadius.circular(10),
            ),
            child: Text(feedback, textAlign: TextAlign.center),
          ),
        ),
      );
    },
  );
}
