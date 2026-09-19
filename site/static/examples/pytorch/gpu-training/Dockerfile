# Image used by the gpu-training.yaml JobSet.
# `pytorch/pytorch` images ship with PyTorch, torchvision and CUDA support;
# extend it with the training script bundled in this directory.
FROM pytorch/pytorch:2.5.1-cuda12.1-cudnn9-runtime

WORKDIR /workspace
COPY train.py train.py
RUN mkdir -p /checkpoints

CMD [ "python", "train.py" ]