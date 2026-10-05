package station

import (
	"context"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

func (s *Service) RecordInspection(
	ctx context.Context,
	planID string,
	request contracts.InspectionRequest,
) (domain.InspectionRecord, error) {
	if err := ctx.Err(); err != nil {
		return domain.InspectionRecord{}, err
	}

	var created domain.InspectionRecord
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		plan, ok := m.GetPlan(planID)
		if !ok {
			return notFoundPlan(planID)
		}
		record, err := domain.NewInspectionRecord(
			s.nextID("inspection"),
			plan,
			m.InspectionsForPlan(planID),
			request.Checkpoint,
			request.Outcome,
			request.InspectedBy,
			request.Note,
			s.now(),
		)
		if err != nil {
			return err
		}
		m.AppendInspection(record)
		created = record
		return nil
	})
	return created, err
}

func (s *Service) ListInspections(
	ctx context.Context,
	planID string,
) ([]domain.InspectionRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var values []domain.InspectionRecord
	err := s.repo.Read(func(m *repository.Mutation) error {
		if _, ok := m.GetPlan(planID); !ok {
			return notFoundPlan(planID)
		}
		values = m.InspectionsForPlan(planID)
		return nil
	})
	return values, err
}
