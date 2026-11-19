#!/bin/sh
# Makes the sound files that the tests in fixtures_test.go use, and their reference: what ffmpeg decodes from each of them, raw interleaved
# 32 bit float little endian (name.f32). The files are in the repository, so that the tests do not need ffmpeg. Run this from this directory to
# make them again (and look at the differences before you commit them).
#
# Every file is 0.2 s at 8000 Hz, a chirp that is another one on every channel.
set -eu
cd "$(dirname "$0")"

chirp() { # channels
  expr=""
  i=0
  while [ "$i" -lt "$1" ]; do
    [ -n "$expr" ] && expr="$expr|"
    expr="${expr}0.6*sin(2*PI*(200+$((3 + i))00*t)*t)"
    i=$((i + 1))
  done
  echo "aevalsrc=$expr:s=8000:d=0.2"
}

make() { # name channels ffmpeg-output-options...
  name=$1; channels=$2; shift 2
  ffmpeg -y -loglevel error -f lavfi -i "$(chirp "$channels")" "$@" "$name"
  ffmpeg -y -loglevel error -i "$name" -f f32le "${name%.*}.f32"
}

make wav_u8_st.wav      2 -c:a pcm_u8
make wav_s16_mono.wav   1 -c:a pcm_s16le
make wav_s16_st.wav     2 -c:a pcm_s16le
make wav_s24_3ch.wav    3 -c:a pcm_s24le
make wav_s32_st.wav     2 -c:a pcm_s32le
make wav_f32_st.wav     2 -c:a pcm_f32le
make wav_f64_st.wav     2 -c:a pcm_f64le
# a-law has no pure-Go decoder: only libsndfile reads it
make wav_alaw_mono.wav  1 -c:a pcm_alaw
make aiff_s8_st.aiff    2 -c:a pcm_s8
make aiff_s16_st.aiff   2 -c:a pcm_s16be
make aiff_s24_st.aiff   2 -c:a pcm_s24be
make aiff_s32_st.aiff   2 -c:a pcm_s32be
make aifc_sowt_st.aifc  2 -c:a pcm_s16le -f aiff
make aifc_fl32_st.aifc  2 -c:a pcm_f32be -f aiff
make flac_s16_st.flac   2 -c:a flac -sample_fmt s16
make flac_s16_mono.flac 1 -c:a flac -sample_fmt s16
make flac_s24_st.flac   2 -c:a flac -sample_fmt s32 -bits_per_raw_sample 24
make ogg_st.ogg         2 -c:a libvorbis
make mp3_22k_st.mp3     2 -c:a libmp3lame -ar 22050
make mp3_44k_st.mp3     2 -c:a libmp3lame -ar 44100
# mpeg 2.5 (8, 11.025 and 12 kHz) is no mp3 for go-mp3: only libsndfile reads it
make mp3_8k_st.mp3      2 -c:a libmp3lame
