# Image used by the gpu-training.yaml JobSet.
# `pytorch/pytorch` images ship with PyTorch, torchvision and CUDA support;
# extend it with the training script bundled in this directory.
FROM pytorch/pytorch:2.5.1-cuda12.1-cudnn9-runtime

WORKDIR /workspace
COPY train.py train.py
# Run as a non-root user. The dataset, checkpoints and script directories are
# writable by this user while CUDA device access is preserved by the runtime.
RUN useradd --create-home --shell /bin/bash traininguser && \
    mkdir -p /checkpoints /data && \
    chown -R traininguser:traininguser /workspace /checkpoints /data
USER traininguser

CMD [ "python", "train.py" ]