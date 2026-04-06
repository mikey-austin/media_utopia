package com.mediautopia.app.ui.components

import android.media.audiofx.Visualizer
import android.util.Log
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.unit.dp
import com.mediautopia.app.ui.theme.Secondary

/**
 * Audio frequency visualizer that renders FFT bars from the currently
 * playing audio using Android's Visualizer API.
 *
 * @param audioSessionId ExoPlayer's audio session ID (0 for output mix)
 * @param barCount Number of frequency bars to display
 */
@Composable
fun AudioVisualizer(
    audioSessionId: Int,
    modifier: Modifier = Modifier,
    barCount: Int = 32,
    barColor: Color = Secondary,
) {
    var fftMagnitudes by remember { mutableStateOf(FloatArray(barCount)) }

    DisposableEffect(audioSessionId) {
        var visualizer: Visualizer? = null
        try {
            visualizer = Visualizer(audioSessionId).apply {
                captureSize = Visualizer.getCaptureSizeRange()[1]
                setDataCaptureListener(
                    object : Visualizer.OnDataCaptureListener {
                        override fun onWaveFormDataCapture(
                            vis: Visualizer, waveform: ByteArray, samplingRate: Int,
                        ) {}

                        override fun onFftDataCapture(
                            vis: Visualizer, fft: ByteArray, samplingRate: Int,
                        ) {
                            val magnitudes = FloatArray(barCount)
                            val step = (fft.size / 2) / barCount
                            for (i in 0 until barCount) {
                                val idx = i * step
                                if (idx + 1 < fft.size) {
                                    val real = fft[idx].toFloat()
                                    val imag = fft[idx + 1].toFloat()
                                    val mag = kotlin.math.sqrt(real * real + imag * imag)
                                    // Normalize to 0..1 range with some headroom.
                                    magnitudes[i] = (mag / 80f).coerceIn(0f, 1f)
                                }
                            }
                            fftMagnitudes = magnitudes
                        }
                    },
                    Visualizer.getMaxCaptureRate() / 2,
                    false,
                    true,
                )
                enabled = true
            }
        } catch (e: Exception) {
            Log.w("AudioVisualizer", "Failed to create Visualizer: ${e.message}")
        }

        onDispose {
            try {
                visualizer?.enabled = false
                visualizer?.release()
            } catch (_: Exception) {}
        }
    }

    Canvas(
        modifier = modifier
            .fillMaxWidth()
            .height(80.dp),
    ) {
        val barWidth = size.width / (barCount * 1.5f)
        val gap = barWidth * 0.5f
        val totalWidth = barCount * (barWidth + gap) - gap
        val offsetX = (size.width - totalWidth) / 2

        for (i in fftMagnitudes.indices) {
            val magnitude = fftMagnitudes[i]
            val barHeight = magnitude * size.height
            val x = offsetX + i * (barWidth + gap)

            drawLine(
                brush = Brush.verticalGradient(
                    colors = listOf(
                        barColor.copy(alpha = 0.8f),
                        barColor.copy(alpha = 0.2f),
                    ),
                    startY = size.height - barHeight,
                    endY = size.height,
                ),
                start = Offset(x + barWidth / 2, size.height),
                end = Offset(x + barWidth / 2, size.height - barHeight),
                strokeWidth = barWidth,
                cap = StrokeCap.Round,
            )
        }
    }
}
