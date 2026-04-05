package com.mediautopia.app.ui

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mediautopia.app.data.repository.ActiveRendererRepository
import com.mediautopia.app.data.repository.NodeRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import javax.inject.Inject

@OptIn(ExperimentalCoroutinesApi::class)
@HiltViewModel
class MainViewModel @Inject constructor(
    activeRendererRepository: ActiveRendererRepository,
    nodeRepository: NodeRepository,
    val snackbarManager: SnackbarManager,
) : ViewModel() {

    val activeRendererName: StateFlow<String?> =
        activeRendererRepository.activeRendererId.flatMapLatest { rendererId ->
            if (rendererId == ActiveRendererRepository.LOCAL_RENDERER_ID) {
                flowOf("This Phone")
            } else {
                nodeRepository.nodes.map { nodes ->
                    nodes[rendererId]?.name
                }
            }
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)
}
