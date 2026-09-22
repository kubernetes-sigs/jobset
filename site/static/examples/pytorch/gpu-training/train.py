"""Distributed training of a ResNet-18 model on CIFAR-10 using PyTorch DDP.

The script is launched with `torchrun` (see gpu-training.yaml), which sets up
the distributed environment. Each worker process runs on one GPU. The workload
spans multiple pods, and the JobSet headless service provides the stable
hostnames needed for the workers to discover each other.
"""

import argparse
import os
from time import time

import torch
import torch.distributed as dist
import torch.nn as nn
import torch.optim as optim
import torchvision.datasets as datasets
import torchvision.transforms as transforms
from torch.nn.parallel import DistributedDataParallel as DDP
from torch.optim.lr_scheduler import StepLR
from torch.utils.data import DataLoader
from torch.utils.data.distributed import DistributedSampler
from torchvision.models import resnet18


def get_device(local_rank):
    if torch.cuda.is_available():
        return torch.device(f"cuda:{local_rank}")
    return torch.device("cpu")


def is_main_process():
    return not dist.is_initialized() or dist.get_rank() == 0


def get_transform():
    normalize = transforms.Normalize(
        mean=[0.4914, 0.4822, 0.4465], std=[0.2023, 0.1994, 0.2010]
    )
    return transforms.Compose(
        [transforms.ToTensor(), normalize]
    )


def main():
    parser = argparse.ArgumentParser(description="Distributed ResNet-18 CIFAR-10 training")
    parser.add_argument("--epochs", type=int, default=10)
    parser.add_argument("--batch-size", type=int, default=128)
    parser.add_argument("--lr", type=float, default=0.1)
    parser.add_argument("--log-interval", type=int, default=10)
    args = parser.parse_args()

    dist.init_process_group(backend="nccl")

    device = get_device(int(os.environ["LOCAL_RANK"]))
    torch.cuda.set_device(device)

    # Each worker pod has its own filesystem, so download the dataset on the
    # local rank 0 of every pod; the other ranks in the same pod read it from
    # the shared pod volume after the barrier.
    if int(os.environ["LOCAL_RANK"]) == 0:
        datasets.CIFAR10(root="/data/cifar10", train=True, download=True)
        datasets.CIFAR10(root="/data/cifar10", train=False, download=True)
    dist.barrier()

    train_dataset = datasets.CIFAR10(
        root="/data/cifar10", train=True, transform=get_transform()
    )
    test_dataset = datasets.CIFAR10(
        root="/data/cifar10", train=False, transform=get_transform()
    )

    train_sampler = DistributedSampler(train_dataset, shuffle=True)
    test_sampler = DistributedSampler(test_dataset, shuffle=False)

    train_loader = DataLoader(
        train_dataset, batch_size=args.batch_size, sampler=train_sampler,
        num_workers=2, pin_memory=True,
    )
    test_loader = DataLoader(
        test_dataset, batch_size=args.batch_size, sampler=test_sampler,
        num_workers=2, pin_memory=True,
    )

    model = resnet18(num_classes=10).to(device)
    model = DDP(model, device_ids=[int(os.environ["LOCAL_RANK"])])

    criterion = nn.CrossEntropyLoss()
    optimizer = optim.SGD(model.parameters(), lr=args.lr, momentum=0.9, weight_decay=5e-4)
    scheduler = StepLR(optimizer, step_size=30, gamma=0.1)

    if is_main_process():
        print(f"Training on {dist.get_world_size()} GPUs", flush=True)

    for epoch in range(args.epochs):
        train_sampler.set_epoch(epoch)
        start = time()
        model.train()
        for step, (images, labels) in enumerate(train_loader):
            images, labels = images.to(device), labels.to(device)
            optimizer.zero_grad()
            outputs = model(images)
            loss = criterion(outputs, labels)
            loss.backward()
            optimizer.step()

            if step % args.log_interval == 0 and is_main_process():
                print(
                    f"Epoch {epoch} step {step}/{len(train_loader)} "
                    f"loss={loss.item():.4f}",
                    flush=True,
                )
        if is_main_process():
            print(f"Epoch {epoch} finished in {time() - start:.1f}s", flush=True)
        scheduler.step()

        # Evaluate on the validation set.
        model.eval()
        correct = total = 0
        with torch.no_grad():
            for images, labels in test_loader:
                images, labels = images.to(device), labels.to(device)
                outputs = model(images)
                _, predicted = outputs.max(1)
                total += labels.size(0)
                correct += (predicted == labels).sum().item()
        if is_main_process():
            print(
                f"Epoch {epoch} test accuracy: {100.0 * correct / total:.2f}%",
                flush=True,
            )

    if is_main_process():
        torch.save(model.module.state_dict(), "/checkpoints/resnet18-cifar10.pth")
        print("Saved checkpoint to /checkpoints/resnet18-cifar10.pth", flush=True)


if __name__ == "__main__":
    main()